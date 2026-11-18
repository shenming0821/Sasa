#!/bin/zsh
# Clean up Fuyu-era permissions and caches after migration to Sasa.
# Run with sudo to remove system-level entries.

set -e

echo "=== Removing Fuyu keychain entries ==="
security delete-certificate -c "Fuyu Dev" ~/Library/Keychains/login.keychain-db 2>/dev/null && echo "deleted Fuyu Dev cert" || echo "no Fuyu Dev cert found"
security delete-generic-password -s "com.shenming.fuyu" 2>/dev/null || true
security delete-internet-password -s "com.shenming.fuyu" 2>/dev/null || true

echo ""
echo "=== Removing Fuyu TCC permissions (requires Full Disk Access) ==="
for bundle in com.shenming.fuyu com.shenming.fuyu.preview; do
  # User TCC
  tccutil reset All "$bundle" 2>/dev/null && echo "reset user TCC: $bundle" || echo "no user TCC: $bundle"
  # System TCC (needs sudo)
  if [ "$EUID" -eq 0 ]; then
    sqlite3 /Library/Application\ Support/com.apple.TCC/TCC.db "DELETE FROM access WHERE client='$bundle';" 2>/dev/null && echo "cleared system TCC: $bundle" || true
  fi
done

echo ""
echo "=== Removing Fuyu LaunchServices registration ==="
lsregister -u /Applications/Fuyu.app 2>/dev/null || true
lsregister -u /Applications/Fuyu\ Preview.app 2>/dev/null || true

echo ""
echo "=== Removing Fuyu caches and logs ==="
rm -rf ~/Library/Caches/Fuyu ~/Library/Logs/Fuyu 2>/dev/null || true

echo ""
echo "=== Done ==="
echo "If a Fuyu.app still exists in /Applications, remove it manually."
