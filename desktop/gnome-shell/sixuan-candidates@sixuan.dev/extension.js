import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import St from 'gi://St';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as IBusManager from 'resource:///org/gnome/shell/misc/ibusManager.js';

export default class extends Extension {
    enable() {
        this._manager = IBusManager.getIBusManager();
        this._popup = this._manager._candidatePopup;
        this._area = this._popup._candidateArea;
        this._markedLabels = new Set();
        this._labelAlignments = new Map();
        this._hadOwnMethod = Object.hasOwn(this._area, 'setCandidates');
        this._originalSetCandidates = this._area.setCandidates;
        const extension = this;
        this._area.setCandidates = function (...args) {
            const result = extension._originalSetCandidates.apply(this, args);
            extension._refreshLabels();
            return result;
        };
        this._bus = this._manager._ibus;
        this._engineSignal = this._bus.connect('global-engine-changed', (_bus, name) => {
            this._setEngine(name);
        });
        // A disconnect clears the manager's engine without emitting engine-changed.
        this._readySignal = this._manager.connect('ready', (_manager, ready) => {
            this._setEngine(ready ? this._manager._currentEngineName : null);
        });
        this._setEngine(this._manager._currentEngineName);
        this._startTheme();
    }

    _setEngine(name) {
        this._active = name === 'sixuan-pinyin' || /^sixuan-[a-z]+-pinyin$/.test(name);
        if (this._active)
            this._popup.add_style_class_name('sixuan-candidates');
        else
            this._popup.remove_style_class_name('sixuan-candidates');
        if (this._active) {
            for (const box of this._area._candidateBoxes) {
                for (const label of [box._indexLabel, box._candidateLabel]) {
                    if (!this._labelAlignments.has(label))
                        this._labelAlignments.set(label, label.y_align);
                    label.y_align = Clutter.ActorAlign.CENTER;
                }
            }
        } else {
            this._restoreAlignments();
        }
        this._refreshLabels();
    }

    _restoreAlignments() {
        for (const [label, alignment] of this._labelAlignments)
            label.y_align = alignment;
        this._labelAlignments.clear();
    }

    _restoreLabels() {
        for (const label of this._markedLabels) {
            // Read current plain text: setCandidates may already have replaced it.
            const text = label.get_text();
            // Markup and explicit attributes are separate in ClutterText.
            // St owns the explicit attributes (including the CSS foreground).
            // Clearing them here loses the color until the next style change.
            label.clutter_text.set_use_markup(false);
            label.set_text(text);
        }
        this._markedLabels.clear();
    }

    _refreshLabels() {
        this._restoreLabels();
        if (!this._active)
            return;
        for (const box of this._area._candidateBoxes) {
            if (!box.visible)
                continue;
            const label = box._candidateLabel;
            const text = label.get_text();
            const match = /^(.*) (〔[^〔〕\n]+〕)$/u.exec(text);
            if (!match)
                continue;
            const body = GLib.markup_escape_text(match[1], -1);
            const source = GLib.markup_escape_text(match[2], -1);
            label.clutter_text.set_markup(
                `${body} <span foreground="${this._sourceColor ?? '#7FE3A8'}" size="small">${source}</span>`);
            this._markedLabels.add(label);
        }
    }

    _startTheme() {
        this._sourceColor = '#7FE3A8';
        this._themeContext = St.ThemeContext.get_for_stage(global.stage);
        this._themeContextSignal = this._themeContext.connect('changed', () => {
            const theme = this._themeContext.get_theme();
            if (theme !== this._loadedTheme) this._reloadTheme();
        });
        this._themePath = GLib.build_filenamev([
            GLib.get_user_state_dir(), 'sixuan-ime', 'theme', 'candidates.css',
        ]);
        this._watchTheme();
        this._reloadTheme();
    }

    _watchTheme() {
        // Monitor the nearest existing ancestor too: the daemon may not have
        // created its state/theme directory when the extension is first enabled.
        let directory = Gio.File.new_for_path(this._themePath).get_parent();
        while (!directory.query_exists(null) && directory.get_parent())
            directory = directory.get_parent();
        const path = directory.get_path();
        if (path === this._watchPath) return;
        this._themeMonitor?.cancel();
        this._watchPath = path;
        this._themeMonitor = directory.monitor_directory(Gio.FileMonitorFlags.WATCH_MOVES, null);
        this._themeMonitor.connect('changed', () => {
            if (this._themeReloadSource) return;
            this._themeReloadSource = GLib.idle_add(GLib.PRIORITY_DEFAULT_IDLE, () => {
                this._themeReloadSource = 0;
                this._watchTheme();
                this._reloadTheme();
                return GLib.SOURCE_REMOVE;
            });
        });
    }

    _reloadTheme() {
        if (this._dynamicStyle) {
            this._loadedTheme.unload_stylesheet(this._dynamicStyle);
            this._dynamicStyle = null;
        }
        // Generated selectors carry this class so they outrank the bundled
        // default stylesheet, which St would otherwise let win.
        this._popup.remove_style_class_name('sx-themed');
        this._loadedTheme = this._themeContext.get_theme();
        this._sourceColor = '#7FE3A8';
        const file = Gio.File.new_for_path(this._themePath);
        if (file.query_exists(null)) {
            try {
                this._loadedTheme.load_stylesheet(file);
                this._dynamicStyle = file;
                this._popup.add_style_class_name('sx-themed');
                const [ok, data] = file.load_contents(null);
                const match = ok && /source-color: (#[0-9A-Fa-f]{6})/.exec(new TextDecoder().decode(data));
                if (match) this._sourceColor = match[1];
            } catch (error) {
                // The bundled default remains loaded by the extension manager.
                console.warn(`Candidate appearance: ${error.message}`);
            }
        }
        this._refreshLabels();
    }

    _stopTheme() {
        this._themeMonitor?.cancel();
        this._themeMonitor = null;
        this._watchPath = null;
        if (this._themeReloadSource) GLib.source_remove(this._themeReloadSource);
        this._themeReloadSource = 0;
        if (this._themeContextSignal) this._themeContext.disconnect(this._themeContextSignal);
        this._themeContextSignal = 0;
        if (this._dynamicStyle) this._loadedTheme.unload_stylesheet(this._dynamicStyle);
        this._dynamicStyle = null;
        this._popup.remove_style_class_name('sx-themed');
        this._loadedTheme = null;
        this._themeContext = null;
    }

    disable() {
        this._stopTheme();
        this._bus.disconnect(this._engineSignal);
        this._manager.disconnect(this._readySignal);
        this._popup.remove_style_class_name('sixuan-candidates');
        if (this._hadOwnMethod)
            this._area.setCandidates = this._originalSetCandidates;
        else
            delete this._area.setCandidates;
        this._restoreLabels();
        this._restoreAlignments();
        this._active = false;
        this._engineSignal = 0;
        this._readySignal = 0;
        this._originalSetCandidates = null;
        this._markedLabels = null;
        this._labelAlignments = null;
        this._area = null;
        this._popup = null;
        this._bus = null;
        this._manager = null;
    }
}
