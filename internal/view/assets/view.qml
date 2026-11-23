// The native wallpaper surface: one layer-shell window on one output, drawing
// the spinewall page in QtWebEngine.
//
// The launcher (launcher.cpp) supplies the context properties below and sets
// and updates the window input mask, because QML Window has no mask property.
// It also reloads the "webview" item after a lock or a resume.
import QtQuick
import QtWebEngine
import org.kde.layershell 1.0 as LayerShell

Window {
    id: win
    visible: true
    color: "#000000"

    LayerShell.Window.scope: "spinewall"
    LayerShell.Window.layer: shellLayer
    LayerShell.Window.anchors: LayerShell.Window.AnchorTop
                             | LayerShell.Window.AnchorBottom
                             | LayerShell.Window.AnchorLeft
                             | LayerShell.Window.AnchorRight
    LayerShell.Window.exclusionZone: -1
    LayerShell.Window.keyboardInteractivity: LayerShell.Window.KeyboardInteractivityNone
    LayerShell.Window.screen: targetScreen

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
