@echo off
rem Relay test for Dens: see README.txt.
cd /d "%~dp0"
del /q kvm-windows.log 2>nul
echo Listening to this computer's sound for 4 minutes. Keep something playing.
echo.
start "" /b relayprobe.exe -quiet -send system -page off -for 240s -log kvm-windows.log
timeout /t 60 /nobreak >nul
echo ^>^>^> SWITCH AWAY now: press the KVM to the other computer, count to 30, switch back.
timeout /t 60 /nobreak >nul
echo ^>^>^> SWITCH AWAY once more: count to 30, switch back.
timeout /t 125 /nobreak >nul
echo.
echo Done. Please send back kvm-windows.log from this folder.
pause
