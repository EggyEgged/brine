package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func subcommands() []string {
	names := make([]string, 0, len(commands)+2)
	for n := range commands {
		names = append(names, n)
	}
	names = append(names, "version", "help")
	sort.Strings(names)
	return names
}

// cmdCompletion prints a completion script for the given shell.
func cmdCompletion(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: brine completion bash|zsh|fish")
	}
	words := strings.Join(subcommands(), " ")
	switch args[0] {
	case "bash":
		fmt.Printf(`# bash completion for brine
_brine() {
    local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}
    case "${COMP_WORDS[1]}:$COMP_CWORD" in
        *:1) COMPREPLY=($(compgen -W "%s" -- "$cur")); return ;;
        mcp:2) COMPREPLY=($(compgen -W "connect disconnect enable test --print-config --debug -v" -- "$cur")); return ;;
        mcp:3) case "$prev" in connect|disconnect) COMPREPLY=($(compgen -W "claude-code claude-desktop" -- "$cur")); return ;; esac ;;
        runtime:2) COMPREPLY=($(compgen -W "update" -- "$cur")); return ;;
        desktop:2) COMPREPLY=($(compgen -W "install remove" -- "$cur")); return ;;
        completion:2) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")); return ;;
        setup:*) COMPREPLY=($(compgen -W "--runtime --yes" -- "$cur")); return ;;
        uninstall:*) COMPREPLY=($(compgen -W "--purge --yes" -- "$cur")); return ;;
        run:*) COMPREPLY=($(compgen -f -X '!*.rbx@(l|lx)' -- "$cur") $(compgen -W "-v" -- "$cur")); return ;;
    esac
}
complete -F _brine brine
`, words)
	case "zsh":
		fmt.Printf(`#compdef brine
# zsh completion for brine
_brine() {
    local -a cmds=(%s)
    if (( CURRENT == 2 )); then
        _describe 'command' cmds
        return
    fi
    case $words[2] in
        mcp)
            if (( CURRENT == 3 )); then
                _values 'mcp command' connect disconnect enable test --print-config --debug
            elif [[ $words[3] == (connect|disconnect) ]]; then
                _values 'client' claude-code claude-desktop
            fi ;;
        runtime) _values 'action' update ;;
        desktop) _values 'action' install remove ;;
        completion) _values 'shell' bash zsh fish ;;
        setup) _values 'option' --runtime --yes ;;
        uninstall) _values 'option' --purge --yes ;;
        run) _files -g '*.rbxl(|x)' ;;
    esac
}
_brine "$@"
`, words)
	case "fish":
		fmt.Println("# fish completion for brine")
		fmt.Println("complete -c brine -f")
		fmt.Printf("complete -c brine -n __fish_use_subcommand -a %q\n", words)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from mcp' -a 'connect disconnect enable test'`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from connect disconnect' -a 'claude-code claude-desktop'`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from runtime' -a update`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from desktop' -a 'install remove'`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from uninstall' -l purge -l yes`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from setup' -l runtime -l yes`)
		fmt.Println(`complete -c brine -n '__fish_seen_subcommand_from run' -F`)
	default:
		return fmt.Errorf("unknown shell %q (supported: bash, zsh, fish)", args[0])
	}
	return nil
}
