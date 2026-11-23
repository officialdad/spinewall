// The native wallpaper surface: one layer-shell window on one output, drawing
// the spinewall page in QtWebEngine.
//
// The launcher (launcher.cpp) supplies the context properties below and sets
// and updates the window input mask, because QML Window has no mask property.
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

    // The plugin used to fire webwallpaper:resume after a pause, and the draw
    // loop never restarted. The native launcher has no such pause: the surface
    // stays alive behind a fullscreen window. A real reload only helps after the
    // surface was hidden, such as a lock or a resume, so reload when the window
    // becomes visible again, never on the first show.
    property bool wasShown: false
    onVisibleChanged: {
        if (visible && wasShown) web.reload();
        if (visible) wasShown = true;
    }
}
