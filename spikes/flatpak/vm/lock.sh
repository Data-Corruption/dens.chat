#!/bin/sh
# Locks the desktop's keyring, so the next secret the app asks for needs it
# unlocked: every Secret Service collection (gnome-keyring, and KWallet's
# ksecretd), and KWallet's wallets through kwalletd where that answers.
cols=$(busctl --user get-property org.freedesktop.secrets /org/freedesktop/secrets \
  org.freedesktop.Secret.Service Collections 2>/dev/null)
if [ -n "$cols" ]; then
  # "ao 2 "/…/login" "/…/session"" becomes the arguments of Lock.
  # shellcheck disable=SC2086
  eval "busctl --user call org.freedesktop.secrets /org/freedesktop/secrets org.freedesktop.Secret.Service Lock $cols" &&
    echo "Locked the Secret Service collections: ${cols#ao }"
fi
for wallet in org.kde.kwalletd6 org.kde.kwalletd5; do
  if busctl --user call "$wallet" /modules/kwalletd6 org.kde.KWallet closeAllWallets >/dev/null 2>&1 ||
    busctl --user call "$wallet" /modules/kwalletd5 org.kde.KWallet closeAllWallets >/dev/null 2>&1; then
    echo "Closed KWallet's wallets through $wallet"
  fi
done
