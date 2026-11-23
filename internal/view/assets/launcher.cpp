// The native wallpaper launcher.
//
// It is a small Qt program, built on the user's machine from this source, that
// runs the spinewall page in QtWebEngine on a layer-shell surface. It exists
// because QML Window has no input mask: the mask needs QWindow::setMask, which
// only C++ can call. The page reports the character's rectangle through the
// server, and this launcher keeps the window's input region in step with it, so
// a click on the character reaches the page and a click elsewhere reaches the
// desktop - which is what the KDE containment patch did.
//
// One process runs one window on one output. `spinewall view --monitor N` starts
// one per monitor.
//
// argv: <page-url> [--output NAME] [--geometry WxH+X+Y] [--layer bottom|background]
//      [--poll-ms N]
#include <QDBusConnection>
#include <QGuiApplication>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QQuickWindow>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QRegion>
#include <QScreen>
#include <QTimer>
#include <QUrl>
#include <QUrlQuery>
#include <QtWebEngineQuick/qtwebenginequickglobal.h>
#include <cstdio>
#include <cstring>

namespace {

struct Options {
    QString pageUrl;
    QString output;
    QString geometry;
    QString screenKey;
    int layer = 1; // 1 = LayerBottom, 0 = LayerBackground
    int pollMs = 400;
    QString apiBase;
    bool listScreens = false;
};

int usage(const char *argv0) {
    std::fprintf(stderr,
                 "usage: %s <page-url> [--output NAME] [--geometry WxH+X+Y] [--screen KEY] "
                 "[--layer bottom|background] [--poll-ms N] [--api URL]\n"
                 "       %s --list-screens\n",
                 argv0, argv0);
    return 64;
}

// parseGeometry reads "WxH+X+Y" into a rect. It returns false when malformed.
bool parseGeometry(const QString &text, QRect &rect) {
    // Accept WxH+X+Y and WxH-X-Y (right/bottom anchored) as KWin reports them.
    int w = 0, h = 0, x = 0, y = 0;
    const QByteArray bytes = text.toLatin1();
    // Parse width and height first.
    const int xpos = bytes.indexOf('x');
    if (xpos <= 0) {
        return false;
    }
    bool okW = false, okH = false;
    w = bytes.left(xpos).toInt(&okW);
    int sign = 1;
    int i = xpos + 1;
    int start = i;
    while (i < bytes.size() && bytes[i] != '+' && bytes[i] != '-') {
        i++;
    }
    h = bytes.mid(start, i - start).toInt(&okH);
    if (!okW || !okH) {
        return false;
    }
    if (i < bytes.size()) {
        sign = bytes[i] == '-' ? -1 : 1;
        i++;
        start = i;
        while (i < bytes.size() && bytes[i] != '+' && bytes[i] != '-') {
            i++;
        }
        bool okX = false;
        x = bytes.mid(start, i - start).toInt(&okX) * sign;
        if (!okX) {
            return false;
        }
    }
    if (i < bytes.size()) {
        sign = bytes[i] == '-' ? -1 : 1;
        i++;
        bool okY = false;
        y = bytes.mid(i).toInt(&okY) * sign;
        if (!okY) {
            return false;
        }
    }
    rect = QRect(x, y, w, h);
    return true;
}

} // namespace

int main(int argc, char **argv) {
    // QtWebEngine must be initialised before the QGuiApplication.
    QtWebEngineQuick::initialize();

    Options opt;
    for (int i = 1; i < argc; i++) {
        const QString arg = QString::fromLocal8Bit(argv[i]);
        if (arg == "--output" && i + 1 < argc) {
            opt.output = QString::fromLocal8Bit(argv[++i]);
        } else if (arg == "--geometry" && i + 1 < argc) {
            opt.geometry = QString::fromLocal8Bit(argv[++i]);
        } else if (arg == "--screen" && i + 1 < argc) {
            opt.screenKey = QString::fromLocal8Bit(argv[++i]);
        } else if (arg == "--layer" && i + 1 < argc) {
            const QString value = QString::fromLocal8Bit(argv[++i]);
            opt.layer = value == "background" ? 0 : 1;
        } else if (arg == "--poll-ms" && i + 1 < argc) {
            opt.pollMs = QString::fromLocal8Bit(argv[++i]).toInt();
        } else if (arg == "--api" && i + 1 < argc) {
            opt.apiBase = QString::fromLocal8Bit(argv[++i]);
        } else if (arg == "--list-screens") {
            opt.listScreens = true;
        } else if (arg.startsWith("--")) {
            return usage(argv[0]);
        } else if (opt.pageUrl.isEmpty()) {
            opt.pageUrl = arg;
        } else {
            return usage(argv[0]);
        }
    }

    if (opt.listScreens) {
        // Enumerate outputs before a full GUI is needed. The line format is
        // NAME<TAB>WxH+X+Y so `spinewall view` can start one window per monitor.
        QGuiApplication app(argc, argv);
        for (QScreen *s : QGuiApplication::screens()) {
            const QRect g = s->geometry();
            std::printf("%s\t%dx%d+%d+%d\n", s->name().toLocal8Bit().constData(), g.width(), g.height(),
                        g.x(), g.y());
        }
        return 0;
    }

    if (opt.pageUrl.isEmpty()) {
        return usage(argv[0]);
    }
    if (opt.apiBase.isEmpty()) {
        opt.apiBase = QStringLiteral("http://127.0.0.1:8777");
    }

    QGuiApplication app(argc, argv);
    app.setApplicationName(QStringLiteral("spinewall-view"));
    // The wallpaper must never quit when the last window would close on a lock.
    app.setQuitOnLastWindowClosed(false);

    // Pick the output. --output names it; --geometry proves the match when the
    // name is empty (KWin reports hex ids for some outputs).
    QScreen *screen = nullptr;
    if (!opt.output.isEmpty()) {
        for (QScreen *s : QGuiApplication::screens()) {
            if (s->name() == opt.output) {
                screen = s;
                break;
            }
        }
    }
    if (!screen && !opt.geometry.isEmpty()) {
        QRect want;
        if (parseGeometry(opt.geometry, want)) {
            for (QScreen *s : QGuiApplication::screens()) {
                if (s->geometry() == want) {
                    screen = s;
                    break;
                }
            }
        }
    }
    if (!screen) {
        screen = QGuiApplication::primaryScreen();
    }
    if (!screen) {
        std::fprintf(stderr, "SPVIEW error no screen\n");
        return 1;
    }
    std::fprintf(stderr, "SPVIEW screen %s %dx%d\n", screen->name().toLocal8Bit().constData(),
                 screen->geometry().width(), screen->geometry().height());

    QQmlApplicationEngine engine;
    engine.rootContext()->setContextProperty(QStringLiteral("pageUrl"), opt.pageUrl);
    engine.rootContext()->setContextProperty(QStringLiteral("targetScreen"),
                                             QVariant::fromValue<QObject *>(screen));
    engine.rootContext()->setContextProperty(QStringLiteral("shellLayer"), opt.layer);
    engine.load(QUrl::fromLocalFile(QCoreApplication::applicationDirPath() + "/view.qml"));
    if (engine.rootObjects().isEmpty()) {
        std::fprintf(stderr, "SPVIEW error no root objects\n");
        return 2;
    }
    auto *window = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
    if (!window) {
        std::fprintf(stderr, "SPVIEW error root is not a QQuickWindow\n");
        return 3;
    }

    // The mask belongs to this window's stage, so the poll names the same screen
    // key the page's URL carries. An empty key is the default stage.
    QString maskUrl = opt.apiBase + "/mask";
    if (!opt.screenKey.isEmpty()) {
        maskUrl += QStringLiteral("?screen=") +
                   QString::fromLatin1(QUrl::toPercentEncoding(opt.screenKey));
    }

    // Poll /mask and keep the input region in step. An unchanged rev is skipped,
    // so the compositor is only told about a real move.
    auto *net = new QNetworkAccessManager(&app);
    auto *timer = new QTimer(&app);
    auto lastRev = new int(-1);
    QObject::connect(timer, &QTimer::timeout, [net, window, lastRev, maskUrl]() {
        QNetworkRequest req{QUrl(maskUrl)};
        req.setAttribute(QNetworkRequest::CacheLoadControlAttribute, QNetworkRequest::AlwaysNetwork);
        QNetworkReply *reply = net->get(req);
        QObject::connect(reply, &QNetworkReply::finished, window, [reply, window, lastRev, maskUrl]() {
            reply->deleteLater();
            if (reply->error() != QNetworkReply::NoError) {
                return;
            }
            const QJsonObject obj = QJsonDocument::fromJson(reply->readAll()).object();
            const int rev = obj.value(QStringLiteral("rev")).toInt(-1);
            if (rev < 0 || rev == *lastRev) {
                return;
            }
            *lastRev = rev;
            if (obj.value(QStringLiteral("full")).toBool(false)) {
                window->setMask(QRegion());
                std::fprintf(stderr, "SPVIEW mask full\n");
                return;
            }
            QRegion region;
            const QJsonArray rects = obj.value(QStringLiteral("rects")).toArray();
            for (const QJsonValue &v : rects) {
                const QJsonObject r = v.toObject();
                region += QRect(r.value(QStringLiteral("x")).toInt(),
                                r.value(QStringLiteral("y")).toInt(),
                                r.value(QStringLiteral("w")).toInt(),
                                r.value(QStringLiteral("h")).toInt());
            }
            // An empty QRegion removes the mask, which would take every click. A
            // region outside the window takes none, so the desktop gets them all.
            if (region.isEmpty()) {
                region = QRegion(-1, -1, 1, 1);
            }
            window->setMask(region);
            std::fprintf(stderr, "SPVIEW mask %d rect(s) rev %d\n", int(rects.size()), rev);
        });
    });
    timer->start(opt.pollMs > 0 ? opt.pollMs : 400);

    // A lock, a suspend or a fullscreen client can leave the page's draw loop
    // dead while the surface stays mapped, so the window never sees a visibility
    // change. Reload on the session's own lock and sleep signals instead. The
    // signals connect to QTimer::start(), a slot with no arguments, so no moc is
    // needed; the timer also folds a burst of signals into one reload. Both edges
    // reload, which is harmless before the lock and fixes the page after it.
    QObject *web = window->findChild<QObject *>(QStringLiteral("webview"));
    auto *reload = new QTimer(&app);
    reload->setSingleShot(true);
    reload->setInterval(1000);
    QObject::connect(reload, &QTimer::timeout, window, [web]() {
        if (web) {
            QMetaObject::invokeMethod(web, "reload");
            std::fprintf(stderr, "SPVIEW reload on resume\n");
        }
    });
    QDBusConnection system = QDBusConnection::systemBus();
    QDBusConnection session = QDBusConnection::sessionBus();
    system.connect(QStringLiteral("org.freedesktop.login1"), QStringLiteral("/org/freedesktop/login1"),
                   QStringLiteral("org.freedesktop.login1.Manager"), QStringLiteral("PrepareForSleep"),
                   reload, SLOT(start()));
    // An empty path matches every session object, since the signal names the
    // session's real path rather than /session/auto.
    system.connect(QStringLiteral("org.freedesktop.login1"), QString(),
                   QStringLiteral("org.freedesktop.login1.Session"), QStringLiteral("Unlock"), reload,
                   SLOT(start()));
    // KDE emits ActiveChanged on /ScreenSaver and /org/freedesktop/ScreenSaver,
    // other lockers on the latter, so match any sender and path.
    session.connect(QString(), QString(), QStringLiteral("org.freedesktop.ScreenSaver"),
                    QStringLiteral("ActiveChanged"), reload, SLOT(start()));
    session.connect(QString(), QString(), QStringLiteral("org.kde.screensaver"),
                    QStringLiteral("ActiveChanged"), reload, SLOT(start()));

    return app.exec();
}
