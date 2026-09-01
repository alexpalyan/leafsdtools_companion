#!/bin/zsh
codesign --sign $CODESIGN_KEY --timestamp --options=runtime "LeafSDTools Companion.app/Contents/MacOS/LeafSDTools_Companion"
zip -r LeafSDTools-Companion-macos-universal.zip "LeafSDTools Companion.app"
xcrun notarytool submit --keychain-profile "$NOTARY_PROFILE" --wait  LeafSDTools-Companion-macos-universal.zip
sha256sum LeafSDTools-Companion-macos-universal.zip > LeafSDTools-Companion-macos-universal.zip.sha256