# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`.

## Global shortcuts on Windows (M7.1)

The helper reads the bound keys' state every 10 ms (`GetAsyncKeyState`) from the desktop user's session, with no keyboard hook, and starts at logon without a window thanks to a manifest (see "Global shortcuts" in `docs/dev/design.md`). This spike checks both on a real desktop before the helper depends on them:

- Does a background process see a key and a mouse side button while a full-screen game has focus, exclusive and borderless, with and without anti-cheat?
- While a program run as administrator has focus, does it see nothing, as User Interface Privilege Isolation says?
- How steady is a 10 ms poll under a game's load, at normal and below-normal priority, and what does it cost?
- Does the manifest's `consoleAllocationPolicy` of `detached` start it without a window from Explorer and from Task Scheduler?

### How

- **`winres`** writes the COFF object that carries the manifest into the build (`rsrc_windows_amd64.syso`, ignored by git), and with `-check` prints the manifest a built `.exe` carries.
- **`shortcuts`** watches only the keys it's given, and logs each press and release with the program in front and whether it runs elevated, and every 10 seconds the poll's timing and its CPU use, to the console and to `%TEMP%\dens-shortcuts-spike.log`. Started without a console, it stops after a minute.

```sh
spikes/shortcuts/build.sh    # out/spikes/shortcuts/dens-shortcuts-spike.exe
```

```powershell
& "$env:TEMP\dens-shortcuts-spike\dens-shortcuts-spike.exe" -keys Mouse4,Mouse5,V,Backquote,F8 [-priority below] [-for 2m]
```

### Results

Windows 11 24H2 (build 26100), on a Ryzen 7 9800X3D (8 cores, 16 threads) with an RTX 4090.

- **A game in front:** V's presses and releases all showed while PRAGMATA ran focused full screen, and in Steam and VS Code. No game with anti-cheat was at hand, so that stays untested; reading key state is passive, which anti-cheat has no reason to block.
- **A program run as administrator in front** (an elevated Windows Terminal): no presses showed, as UIPI says. The settings will say so.
- **Timing:** at normal and at below-normal priority, with the game running, reads came at a median and p99 of 10 ms. Across some 15,000 reads one gap reached 20 ms, and none went over. Normal priority stays as cheap insurance against a game that loads every core.
- **Cost:** 0.2% to 1% of one core while reading, a little more with a game running. The helper reads only during calls.
- **No window:** started from Explorer, from Task Scheduler (`schtasks /Run`) and from WSL, the manifest left it without a console (`console none`), and no window appeared. Started from PowerShell, it ran in that console.
- **The mouse's side buttons:** none showed in this run. The M7.1 manual test then toggled mute with a side button from a game, through the helper.
- For a moment during Alt+Tab, and while UAC's prompt is up on its secure desktop, no window is in front (`GetForegroundWindow` returns none). Windows documents that key reads see nothing while another desktop is active.
