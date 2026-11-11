# Demo of llm-filesystem v1.0.2. Re-render with: vhs docs/demo/llm-filesystem.tape
# Needs llm-filesystem on PATH, plus vhs, ttyd and ffmpeg.
Output docs/demo/llm-filesystem.gif

Set Shell "bash"
Set FontSize 18
Set Width 1100
Set Height 620
Set Padding 20
Set TypingSpeed 45ms

# Fixture, built off camera.
Hide
Type "rm -rf /tmp/demo && mkdir -p /tmp/demo/src && cd /tmp/demo && printf 'package main\n\nfunc main() {}\n' > src/main.go && echo '# demo' > README.md && clear"
Enter
Show

Type "llm-filesystem --version"
Enter
Sleep 2s

Type "llm-filesystem read-file --path README.md"
Enter
Sleep 3s

Type "llm-filesystem search-code --pattern main --path ."
Enter
Sleep 4s

Type "llm-filesystem list-directory --path . --axi"
Enter
Sleep 5s
