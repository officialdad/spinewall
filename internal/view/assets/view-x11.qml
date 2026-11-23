// The native wallpaper surface on a real X11 session: one desktop-type window,
// placed on one output, drawing the spinewall page in QtWebEngine.
//
// X11 has no layer-shell, so the window asks the window manager to treat it as
// the desktop, keep it at the bottom, and never take focus. The launcher passes
// the output's geometry in targetX/targetY/targetWidth/targetHeight. This path
// is best effort and unverified on real hardware; see docs/desktops.md.
import QtQuick
import QtWebEngine

Window {
    id: win
    visible: true
    color: "#000000"

    x: targetX
    y: targetY
    width: targetWidth
    height: targetHeight
    flags: Qt.Desktop | Qt.FramelessWindowHint | Qt.WindowStaysOnBottomHint
         | Qt.WindowDoesNotAcceptFocus

    WebEngineView {
        id: web
        objectName: "webview"
        anchors.fill: parent
        url: pageUrl
        backgroundColor: "#000000"
        settings.webGLEnabled: true
        settings.localContentCanAccessRemoteUrls: true
        settings.localStorageEnabled: true
        settings.playbackRequiresUserGesture: false
    }
}
