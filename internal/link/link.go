// Package link turns what a desktop environment, browser or user hands to
// `brine run` (a roblox-studio: link, a sign-in callback, a file or a
// roblox.com web address) into Studio's command line.
//
// Links come from web pages, so they are untrusted input. Studio is started
// through Wine, which flattens argv into one Windows command line; a link
// containing spaces or quotes could therefore smuggle extra Studio arguments
// (for example -localPlaceFile) past the single argument we intend. Anything
// that doesn't look like a plain Roblox link is rejected.
package link

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"brine/internal/netx"
	"brine/internal/wine"
)

// MaxLength bounds a link; real ones are a few hundred bytes.
const MaxLength = 4096

// Kind says what a target is.
type Kind int

const (
	None   Kind = iota // nothing given: just start Studio
	Studio             // roblox-studio: link
	Auth               // roblox-studio-auth: sign-in callback
	File               // local place file
)

// Result is a resolved target.
type Result struct {
	Kind Kind
	// Args are the arguments for RobloxStudioBeta.exe.
	Args []string
	// Canonical is the form to hand to another brine process: the link
	// itself, a generated roblox-studio: link for a web address, or an
	// absolute path for a file.
	Canonical string
}

// Lookup resolves a universe ID to its starting place ID.
type Lookup func(universeID string) (placeID string, err error)

var (
	studioLink = regexp.MustCompile(`^roblox-studio:[A-Za-z0-9+:._~%/=&?@!*,;-]+$`)
	authLink   = regexp.MustCompile(`^roblox-studio-auth:[A-Za-z0-9+:._~%/=&?@!*,;#-]*$`)
	gamePage   = regexp.MustCompile(`^/(?:[a-z]{2}(?:-[a-z]{2})?/)?games/(\d+)(?:/|$)`)
	creatorURL = regexp.MustCompile(`^/dashboard/creations/experiences/(\d+)(?:/places/(\d+))?(?:/|$)`)
)

// Resolve interprets arg. lookup may be nil if no network lookups are wanted
// (a creator-dashboard address then fails with a clear error).
func Resolve(arg string, lookup Lookup) (*Result, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return &Result{Kind: None}, nil
	}
	if len(arg) > MaxLength {
		return nil, errors.New("link is too long")
	}
	for _, r := range arg {
		if r < 0x20 || r == 0x7f {
			return nil, errors.New("link contains control characters")
		}
	}
	lower := strings.ToLower(arg)
	// URL schemes are case-insensitive; hand Studio the lowercase form.
	for _, scheme := range []string{"roblox-studio-auth:", "roblox-studio:"} {
		if strings.HasPrefix(lower, scheme) {
			arg = scheme + arg[len(scheme):]
			break
		}
	}
	switch {
	case strings.HasPrefix(lower, "roblox-studio-auth:"):
		if !authLink.MatchString(arg) {
			return nil, errors.New("not a valid roblox-studio-auth: link")
		}
		return &Result{Kind: Auth, Args: []string{arg}, Canonical: arg}, nil
	case strings.HasPrefix(lower, "roblox-studio:"):
		if !studioLink.MatchString(arg) {
			return nil, errors.New("not a valid roblox-studio: link (it contains characters Roblox links never use)")
		}
		return &Result{Kind: Studio, Args: []string{arg}, Canonical: arg}, nil
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return webLink(arg, lookup)
	case strings.HasPrefix(lower, "file://"):
		u, err := url.Parse(arg)
		if err != nil {
			return nil, fmt.Errorf("bad file link: %w", err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return nil, fmt.Errorf("file link points at another host (%s)", u.Host)
		}
		return fileResult(u.Path)
	}
	if i := strings.Index(arg, ":"); i > 0 && !strings.ContainsAny(arg[:i], "/.") {
		return nil, fmt.Errorf("don't know how to open %q links", arg[:i])
	}
	return fileResult(arg)
}

func fileResult(path string) (*Result, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".rbxl", ".rbxlx":
		return &Result{Kind: File, Args: []string{"-task", "EditFile", "-localPlaceFile", wine.WinPath(abs)}, Canonical: abs}, nil
	}
	return nil, fmt.Errorf("%s: Studio opens .rbxl and .rbxlx places", path)
}

// webLink maps a roblox.com experience address to an "Edit in Studio" link.
func webLink(arg string, lookup Lookup) (*Result, error) {
	u, err := url.Parse(arg)
	if err != nil {
		return nil, fmt.Errorf("bad web address: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	var placeID, universeID string
	switch host {
	case "www.roblox.com", "roblox.com", "web.roblox.com":
		m := gamePage.FindStringSubmatch(u.Path)
		if m == nil {
			return nil, errors.New("this roblox.com address isn't an experience page (expected roblox.com/games/<id>)")
		}
		placeID = m[1]
	case "create.roblox.com":
		m := creatorURL.FindStringSubmatch(u.Path)
		if m == nil {
			return nil, errors.New("this create.roblox.com address isn't an experience (expected .../dashboard/creations/experiences/<id>)")
		}
		universeID, placeID = m[1], m[2]
		if placeID == "" {
			if lookup == nil {
				return nil, errors.New("resolving a creator dashboard address needs network access")
			}
			if placeID, err = lookup(universeID); err != nil {
				return nil, fmt.Errorf("could not find the starting place of experience %s: %w", universeID, err)
			}
		}
	default:
		return nil, fmt.Errorf("don't know how to open links to %s", host)
	}
	s := "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:" + placeID
	if universeID != "" {
		s += "+universeId:" + universeID
	}
	return &Result{Kind: Studio, Args: []string{s}, Canonical: s}, nil
}

// NetLookup resolves a universe ID to its starting place with Roblox's
// public games API.
func NetLookup(universeID string) (string, error) {
	var r struct {
		Data []struct {
			RootPlaceID int64 `json:"rootPlaceId"`
		} `json:"data"`
	}
	if err := netx.GetJSONTimeout("https://games.roblox.com/v1/games?universeIds="+url.QueryEscape(universeID), 8*time.Second, &r); err != nil {
		return "", err
	}
	if len(r.Data) == 0 || r.Data[0].RootPlaceID == 0 {
		return "", errors.New("no such experience (or it is private)")
	}
	return strconv.FormatInt(r.Data[0].RootPlaceID, 10), nil
}
