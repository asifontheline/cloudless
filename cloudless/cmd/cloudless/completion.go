package main

import (
	"fmt"
	"log"
	"strings"
)

// Shell completion (Q6): `cloudless completion <bash|zsh|fish>` prints a
// script to stdout — standard practice (kubectl, gh, docker) so it works
// with `source <(cloudless completion bash)` or the shell's own completion
// directory, no separate install step.

// cliCommands is the top-level command list completion offers. Kept as a
// plain slice here rather than derived from the switch in main() — Go has
// no reflection-free way to enumerate a switch's cases, and duplicating a
// short list beats machinery for something this static.
var cliCommands = []string{
	"up", "serve", "status", "usage", "ledger", "keys", "savings", "capacity",
	"vault", "restore", "backup", "ext", "models", "share", "nodes", "audit",
	"token", "bench", "resolve", "chat", "config", "completion",
}

func completionCmd(args []string) {
	if len(args) != 1 {
		log.Fatal("usage: cloudless completion <bash|zsh|fish>")
	}
	words := strings.Join(cliCommands, " ")
	switch args[0] {
	case "bash":
		fmt.Printf(`# cloudless bash completion
# Install: source <(cloudless completion bash)
# Or:      cloudless completion bash > /etc/bash_completion.d/cloudless
_cloudless_completions() {
  COMPREPLY=($(compgen -W "%s" -- "${COMP_WORDS[1]}"))
}
complete -F _cloudless_completions cloudless
`, words)
	case "zsh":
		fmt.Printf(`# cloudless zsh completion
# Install: source <(cloudless completion zsh)
# Or:      cloudless completion zsh > "${fpath[1]}/_cloudless"
#compdef cloudless
_cloudless() {
  local -a commands
  commands=(%s)
  _describe 'command' commands
}
_cloudless
`, words)
	case "fish":
		fmt.Printf(`# cloudless fish completion
# Install: cloudless completion fish | source
# Or:      cloudless completion fish > ~/.config/fish/completions/cloudless.fish
complete -c cloudless -f -n '__fish_use_subcommand' -a '%s'
`, words)
	default:
		log.Fatalf("unknown shell %q — bash, zsh, fish", args[0])
	}
}
