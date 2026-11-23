// The native wallpaper surface on a real X11 session: one desktop-type window,
// placed on one output, drawing the spinewall page in QtWebEngine.
//
// X11 has no layer-shell, so the window is a normal frameless window kept at
// the bottom that never takes focus. Qt.Desktop is not used: the xcb plugin
// adopts the root window for it, so every monitor would share one surface. The
// launcher marks the window _NET_WM_WINDOW_TYPE_DESKTOP before it maps, which is
// why it starts hidden. It sets no input mask: on X11 that would clip the
// drawing too, so the whole window takes clicks. The launcher passes the output's geometry in
// targetX/targetY/targetWidth/targetHeight. This path is best effort and
// unverified on real hardware; see docs/desktops.md.
import QtQuick
import QtWebEngine

Window {
    id: win
    visible: false
    color: "#000000"

    x: targetX
    y: targetY
    width: targetWidth
    height: targetHeight
    flags: Qt.Window | Qt.FramelessWindowHint | Qt.WindowStaysOnBottomHint
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
