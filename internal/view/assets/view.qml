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

    // KWin derives a layer-shell surface's window type from its scope, and treats
    // an unknown scope as a normal window, which Peek at Desktop (Meta+D) hides.
    // The scope "desktop" makes KWin classify the surface as a desktop window,
    // the same type plasmashell's wallpaper has, and Peek leaves desktop windows
    // shown. This is why the old Plasma wallpaper backend never disappeared.
    LayerShell.Window.scope: "desktop"
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
        // A lost GPU context or a dead render process leaves the surface drawing
        // nothing while the window stays mapped. Reload the page so the draw
        // loop comes back, as the page already does on webglcontextlost.
        onRenderProcessTerminated: reload()
    }
}
