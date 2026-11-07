#include "native.h"
#include <errno.h>
#include <fcntl.h>
#include <gio/gio.h>
#include <glob.h>
#include <gtk/gtk.h>
#include <ibus.h>
#include <linux/input.h>
#include <math.h>
#include <string.h>
#include <unistd.h>
extern void goDrain(void);
extern void goKey(char *, int, int);
extern char *goCommand(char *);
extern void goDelivered(uint64_t, int, char *);
extern void goLocked(int);
extern void goPosition(int, int);
#define ENGINE NAME_ENGINE
#define SPACE_PATH NAME_SPACE
static IBusBus *bus;
static IBusFactory *factory;
static GMainLoop *loop;
static GDBusConnection *session_bus;
static GSettings *space_settings;
static int gui = 0;
static guint owner = 0;
static char *previous = NULL, *target = NULL, *desired = NULL;
static uint64_t current_generation = 0;
static int pending = 0, selected = 0, ready = 0, final_text = 0, restoring = 0,
           committed = 0;
typedef struct {
  IBusEngine parent;
  gboolean focused, enabled;
  guint purpose;
} VoiceEngine;
typedef struct {
  IBusEngineClass parent;
} VoiceEngineClass;
G_DEFINE_TYPE(VoiceEngine, voice_engine, IBUS_TYPE_ENGINE)
static VoiceEngine *active;
static int voice_commits, voice_preedits, composition_lost;
int native_voice_commits(void) { return voice_commits; }
int native_voice_preedits(void) { return voice_preedits; }
static void try_text(void);
static void restore_bridge(int, const char *);
static void focus_in(IBusEngine *e) {
  active = (VoiceEngine *)e;
  active->focused = TRUE;
  try_text();
}
static void focus_in_id(IBusEngine *e, const gchar *path, const gchar *client) {
  focus_in(e);
}
static void invalidate_composition(IBusEngine *e) {
  if ((VoiceEngine *)e == active && pending && selected && !restoring &&
      !committed && desired && *desired)
    composition_lost = 1;
}
static void reset(IBusEngine *e) { invalidate_composition(e); }
static void focus_out(IBusEngine *e) {
  invalidate_composition(e);
  ((VoiceEngine *)e)->focused = FALSE;
}
static void focus_out_id(IBusEngine *e, const gchar *path) { focus_out(e); }
static void enable(IBusEngine *e) {
  active = (VoiceEngine *)e;
  active->enabled = TRUE;
  try_text();
}
static void disable(IBusEngine *e) { ((VoiceEngine *)e)->enabled = FALSE; }
static void content_type(IBusEngine *e, guint purpose, guint hints) {
  ((VoiceEngine *)e)->purpose = purpose;
}
extern int goInputKey(int, int);
int native_public_key(unsigned int keyval, unsigned int keycode, unsigned int state) {
  if (!goPublicBuild()) return FALSE;
  int code = keyval == IBUS_Control_L ? 29 : keyval == IBUS_Control_R ? 97 :
             keyval == IBUS_Escape ? 1 : keyval == IBUS_space ? 57 : 1000 + keycode;
  return goInputKey(code, state & IBUS_RELEASE_MASK ? 0 : 1);
}
static gboolean process_key(IBusEngine *e, guint keyval, guint keycode,
                            guint state) {
  VoiceEngine *voice = (VoiceEngine *)e;
  if (voice->purpose == IBUS_INPUT_PURPOSE_PASSWORD || voice->purpose == IBUS_INPUT_PURPOSE_PIN) return FALSE;
  return native_public_key(keyval, keycode, state);
}
static void engine_destroy(IBusObject *object) {
  if (active == (VoiceEngine *)object)
    active = NULL;
  IBUS_OBJECT_CLASS(voice_engine_parent_class)->destroy(object);
}
static void voice_engine_class_init(VoiceEngineClass *klass) {
  IBusEngineClass *e = IBUS_ENGINE_CLASS(klass);
  e->focus_in = focus_in;
  e->focus_out = focus_out;
  e->focus_in_id = focus_in_id;
  e->focus_out_id = focus_out_id;
  e->enable = enable;
  e->disable = disable;
  e->set_content_type = content_type;
  e->process_key_event = process_key;
  e->reset = reset;
  IBUS_OBJECT_CLASS(klass)->destroy = engine_destroy;
}
static void voice_engine_init(VoiceEngine *e) {
  e->focused = FALSE;
  e->enabled = FALSE;
  e->purpose = 0;
}
char *native_context(void) {
  return bus ? ibus_bus_current_input_context(bus) : g_strdup("");
}
char *native_engine(void) {
  if (!bus) {
    ibus_init();
    bus = ibus_bus_new();
  }
  IBusEngineDesc *e = ibus_bus_get_global_engine(bus);
  char *name = g_strdup(e ? ibus_engine_desc_get_name(e) : "");
  if (e)
    g_object_unref(e);
  return name;
}
int native_restore(const char *name) {
  if (!bus) {
    ibus_init();
    bus = ibus_bus_new();
  }
  return name && *name && strcmp(name, ENGINE) &&
         ibus_bus_set_global_engine(bus, name);
}
typedef struct {
  uint64_t gen;
  int success;
  char *reason;
} Restore;
static void restore_finished(GObject *object, GAsyncResult *result,
                             gpointer data) {
  Restore *r = data;
  GError *error = NULL;
  gboolean ok =
      ibus_bus_set_global_engine_async_finish(IBUS_BUS(object), result, &error);
  if (pending && current_generation == r->gen) {
    pending = 0;
    char *reason = r->reason;
    if (!ok)
      reason = r->success ? "文字已输入，请切回原输入法" : "请切回原输入法";
    goDelivered(r->gen, r->success, reason);
  }
  g_clear_error(&error);
  g_free(r->reason);
  g_free(r);
}
static void restore_bridge(int success, const char *reason) {
  if (!pending || restoring)
    return;
  restoring = 1;
  if (active && selected)
    ibus_engine_update_preedit_text_with_mode(IBUS_ENGINE(active),
                                              ibus_text_new_from_string(""), 0,
                                              FALSE, IBUS_ENGINE_PREEDIT_CLEAR);
  Restore *r = g_new0(Restore, 1);
  r->gen = current_generation;
  r->success = success;
  r->reason = g_strdup(reason);
  ibus_bus_set_global_engine_async(bus, previous, 2000, NULL, restore_finished,
                                   r);
}
static gboolean commit_restore(gpointer data) {
  uint64_t *gen = data;
  if (pending && current_generation == *gen)
    restore_bridge(1, "");
  g_free(gen);
  return G_SOURCE_REMOVE;
}
static void try_text(void) {
  if (!pending || restoring || !selected || committed)
    return;
  char *context = native_context();
  int valid = context && target && !strcmp(context, target);
  g_free(context);
  if (!valid) {
    restore_bridge(0, "输入焦点已变化，结果已保留");
    return;
  }
  if (composition_lost) {
    if (final_text)
      restore_bridge(0, "输入框重置了实时文字，完整结果已保留，可复制上次结果");
    return;
  }
  if (!active || !active->enabled || !active->focused)
    return;
  ready = 1;
  if (active->purpose == IBUS_INPUT_PURPOSE_PASSWORD ||
      active->purpose == IBUS_INPUT_PURPOSE_PIN) {
    restore_bridge(0, "受保护输入框");
    return;
  }
  IBusText *text = ibus_text_new_from_string(desired ? desired : "");
  if (final_text) {
    committed = 1;
    voice_commits++;
    // Commit while the client still owns the composition range. Clearing it
    // first can make rich editors treat the final result as a fresh insertion.
    ibus_engine_commit_text(IBUS_ENGINE(active), text);
    ibus_engine_update_preedit_text_with_mode(IBUS_ENGINE(active),
                                              ibus_text_new_from_string(""), 0,
                                              FALSE, IBUS_ENGINE_PREEDIT_CLEAR);
    uint64_t *gen = g_new(uint64_t, 1);
    *gen = current_generation;
    g_timeout_add(25, commit_restore, gen);
  } else {
    voice_preedits++;
    guint length = g_utf8_strlen(desired ? desired : "", -1);
    ibus_text_append_attribute(text, IBUS_ATTR_TYPE_UNDERLINE,
                               IBUS_ATTR_UNDERLINE_SINGLE, 0, length);
    ibus_engine_update_preedit_text_with_mode(IBUS_ENGINE(active), text, length,
                                              length > 0,
                                              IBUS_ENGINE_PREEDIT_CLEAR);
  }
}
static void selected_engine(GObject *object, GAsyncResult *result,
                            gpointer data) {
  uint64_t gen = *(uint64_t *)data;
  g_free(data);
  GError *error = NULL;
  int ok =
      ibus_bus_set_global_engine_async_finish(IBUS_BUS(object), result, &error);
  if (pending && gen == current_generation && !restoring) {
    if (ok) {
      selected = 1;
      try_text();
    } else
      restore_bridge(0, "无法连接当前输入框");
  }
  g_clear_error(&error);
}
static gboolean expire(gpointer data) {
  uint64_t gen = *(uint64_t *)data;
  g_free(data);
  if (pending && gen == current_generation && !ready)
    restore_bridge(0, "当前应用未接收输入");
  return G_SOURCE_REMOVE;
}
int native_begin(uint64_t gen) {
  if (pending)
    return 0;
  g_free(previous);
  previous = native_engine();
  g_free(target);
  target = native_context();
  if (!previous || !*previous || !strcmp(previous, ENGINE) || !target ||
      !*target)
    return 0;
  g_free(desired);
  desired = g_strdup("");
  pending = 1;
  selected = ready = final_text = restoring = committed = 0;
  current_generation = gen;
  voice_commits = voice_preedits = composition_lost = 0;
  uint64_t *g = g_new(uint64_t, 1);
  *g = gen;
  ibus_bus_set_global_engine_async(bus, ENGINE, 3000, NULL, selected_engine, g);
  g = g_new(uint64_t, 1);
  *g = gen;
  g_timeout_add(3500, expire, g);
  return 1;
}
void native_text(const char *text, int final) {
  if (!pending || restoring || committed)
    return;
  g_free(desired);
  desired = g_strdup(text);
  final_text = final;
  try_text();
  if (final && !committed) {
    uint64_t *g = g_new(uint64_t, 1);
    *g = current_generation;
    ready = 0;
    g_timeout_add(3500, expire, g);
  }
}
void native_cancel(void) { restore_bridge(0, "已取消输入"); }
void native_space(int enabled) {
  if (!space_settings) {
    GSettings *parent =
        g_settings_new("org.gnome.settings-daemon.plugins.media-keys");
    space_settings = g_settings_new_with_path(
        "org.gnome.settings-daemon.plugins.media-keys.custom-keybinding",
        SPACE_PATH);
    gchar **paths = g_settings_get_strv(parent, "custom-keybindings");
    gboolean found = FALSE;
    guint n = g_strv_length(paths);
    for (guint i = 0; i < n; i++)
      if (!strcmp(paths[i], SPACE_PATH))
        found = TRUE;
    if (!found) {
      gchar **updated = g_new0(gchar *, n + 2);
      for (guint i = 0; i < n; i++)
        updated[i] = paths[i];
      updated[n] = (char *)SPACE_PATH;
      g_settings_set_strv(parent, "custom-keybindings",
                          (const gchar *const *)updated);
      g_free(updated);
    }
    g_strfreev(paths);
    g_settings_set_string(space_settings, "name",
                          "思玄语音：空格结束录音（仅录音时启用）");
    g_settings_set_string(space_settings, "command",
                          NAME_STOP);
    g_object_unref(parent);
  }
  g_settings_set_string(space_settings, "binding", enabled ? "space" : "");
  g_settings_sync();
}
/* Native event devices, read-only and never grabbed. */
typedef struct {
  int fd;
  char *path;
} Keyboard;
static GHashTable *keyboards;
static int key_count = 0;
#define BITSET(a, n)                                                           \
  ((a[(n) / (8 * sizeof(unsigned long))] >>                                    \
    ((n) % (8 * sizeof(unsigned long)))) &                                     \
   1)
static gboolean key_ready(GIOChannel *channel, GIOCondition condition,
                          gpointer data) {
  Keyboard *k = data;
  struct input_event input;
  ssize_t n;
  int bad = condition & (G_IO_HUP | G_IO_ERR);
  while (!bad &&
         (n = read(k->fd, &input, sizeof(input))) == (ssize_t)sizeof(input)) {
    if (input.type == EV_KEY)
      goKey(k->path, input.code, input.value);
    else if (input.type == EV_SYN && input.code == SYN_DROPPED)
      goKey(k->path, -1, 0);
  }
  if (!bad && n < 0 && errno != EAGAIN && errno != EINTR)
    bad = 1;
  if (bad) {
    goKey(k->path, -1, 0);
    g_hash_table_remove(keyboards, k->path);
    close(k->fd);
    g_free(k->path);
    g_free(k);
    key_count--;
    return G_SOURCE_REMOVE;
  }
  return G_SOURCE_CONTINUE;
}
static gboolean scan_keys(gpointer unused) {
  glob_t matches;
  if (glob("/dev/input/event*", 0, NULL, &matches))
    return G_SOURCE_CONTINUE;
  for (size_t i = 0; i < matches.gl_pathc; i++) {
    char *path = matches.gl_pathv[i];
    if (g_hash_table_contains(keyboards, path))
      continue;
    int fd = open(path, O_RDONLY | O_NONBLOCK | O_CLOEXEC);
    if (fd < 0)
      continue;
    unsigned long bits[(KEY_MAX + 8 * sizeof(long)) / (8 * sizeof(long))];
    memset(bits, 0, sizeof(bits));
    if (ioctl(fd, EVIOCGBIT(EV_KEY, sizeof(bits)), bits) < 0 ||
        !BITSET(bits, KEY_A) || !BITSET(bits, KEY_SPACE) ||
        (!BITSET(bits, KEY_LEFTCTRL) && !BITSET(bits, KEY_RIGHTCTRL))) {
      close(fd);
      continue;
    }
    Keyboard *k = g_new0(Keyboard, 1);
    k->fd = fd;
    k->path = g_strdup(path);
    g_hash_table_add(keyboards, k->path);
    key_count++;
    memset(bits, 0, sizeof(bits));
    if (ioctl(fd, EVIOCGKEY(sizeof(bits)), bits) >= 0)
      for (int code = 0; code <= KEY_MAX; code++)
        if (BITSET(bits, code))
          goKey(k->path, code, 3);
    GIOChannel *io = g_io_channel_unix_new(fd);
    g_io_add_watch(io, G_IO_IN | G_IO_HUP | G_IO_ERR, key_ready, k);
    g_io_channel_unref(io);
  }
  globfree(&matches);
  return G_SOURCE_CONTINUE;
}
int native_keyboards(void) { return key_count; }
static void screen_locked(GDBusConnection *c, const gchar *sender,
                          const gchar *path, const gchar *iface,
                          const gchar *signal, GVariant *params,
                          gpointer data) {
  gboolean locked;
  g_variant_get(params, "(b)", &locked);
  goLocked(locked);
}
static void disconnected(IBusBus *b, gpointer unused) { native_quit(); }
// Restarting this service drops its engines and IBus silently falls back to
// another engine while GNOME still shows Sixuan as current; reselect it.
static void restore_pinyin(void) {
  GSettingsSchema *schema = g_settings_schema_source_lookup(
      g_settings_schema_source_get_default(), "org.gnome.desktop.input-sources",
      TRUE);
  if (!schema)
    return;
  g_settings_schema_unref(schema);
  GSettings *settings = g_settings_new("org.gnome.desktop.input-sources");
  GVariant *recent = g_settings_get_value(settings, "mru-sources");
  const char *type = NULL, *name = NULL;
  if (g_variant_n_children(recent))
    g_variant_get_child(recent, 0, "(&s&s)", &type, &name);
  if (type && !strcmp(type, "ibus") && !strcmp(name, NAME_PINYIN))
    ibus_bus_set_global_engine_async(bus, name, 3000, NULL, NULL, NULL);
  g_variant_unref(recent);
  g_object_unref(settings);
}
static const char *xml =
    "<node><interface name='%s'><method name='PinyinTrace'><arg type='s' direction='out'/></method>"
    "<method name='PinyinTraceStart'><arg type='s' direction='out'/></method>"
    "<method name='PinyinTraceStop'><arg type='s' direction='out'/></method><method name='Toggle'><arg "
    "type='s' direction='out'/></method><method name='Stop'><arg type='s' "
    "direction='out'/></method><method name='Cancel'><arg type='s' "
    "direction='out'/></method><method name='Settings'><arg type='s' "
    "direction='out'/></method><method name='Reload'><arg type='s' "
    "direction='out'/></method><method name='RegisterPinyin'><arg type='s' "
    "direction='out'/></method><method name='SelectPinyin'><arg type='s' "
    "direction='out'/></method><method name='Status'><arg type='s' "
    "direction='out'/></method><method name='Last'><arg type='s' "
    "direction='out'/></method><method name='CopyLast'><arg type='s' "
    "direction='out'/></method></interface></node>";
static void command_call(GDBusConnection *connection, const gchar *sender,
                         const gchar *path, const gchar *iface,
                         const gchar *method, GVariant *params,
                         GDBusMethodInvocation *invocation, gpointer unused) {
  char *result = goCommand((char *)method);
  g_dbus_method_invocation_return_value(invocation,
                                        g_variant_new("(s)", result));
  free(result);
}
static const GDBusInterfaceVTable commands = {.method_call = command_call};
static gboolean drain(gpointer unused) {
  goDrain();
  return G_SOURCE_REMOVE;
}
void native_post(void) { g_idle_add(drain, NULL); }
void native_run(void) {
  loop = g_main_loop_new(NULL, FALSE);
  g_main_loop_run(loop);
}
void native_quit(void) {
  if (loop)
    g_main_loop_quit(loop);
}
int native_init(int keys) {
  g_setenv("GDK_BACKEND", "x11", TRUE);
  g_setenv("GTK_IM_MODULE", "gtk-im-context-simple", TRUE);
  ibus_init();
  if (!bus)
    bus = ibus_bus_new();
  if (!ibus_bus_is_connected(bus))
    return 0;
  factory = ibus_factory_new(ibus_bus_get_connection(bus));
  ibus_factory_add_engine(factory, ENGINE, voice_engine_get_type());
  ibus_factory_add_engine(factory, NAME_PINYIN,
                          native_pinyin_type());
  IBusComponent *component = ibus_component_new(
      NAME_COMPONENT, "思玄语音输入", NAME_VERSION,
      NAME_LICENSE, "Sixuan", "", "", "");
  IBusEngineDesc *engine = ibus_engine_desc_new(
      ENGINE, "思玄语音", "Go 原生语音输入", "zh", NAME_LICENSE, "Sixuan",
      "audio-input-microphone", "default");
  ibus_component_add_engine(component, engine);
  char *cli =
      goPublicBuild() ? g_strdup("/usr/bin/sixuan-ime") : g_build_filename(g_get_home_dir(), ".local", "bin", NAME_CLI, NULL);
  char *quoted = g_shell_quote(cli),
       *setup = g_strdup_printf("%s settings", quoted);
  ibus_component_add_engine(
      component,
      ibus_engine_desc_new_varargs(
          "name", NAME_PINYIN, "longname", NAME_PINYIN_TITLE,
          "description", NAME_PINYIN_DESCRIPTION, "language", "zh",
          "license", NAME_LICENSE, "author", "Sixuan", "icon", "input-keyboard",
          "layout", "default", "setup", setup, NULL));
  g_free(cli);
  g_free(quoted);
  g_free(setup);
  if (!ibus_bus_register_component(bus, component))
    return 0;
  g_object_unref(component);
  restore_pinyin();
  g_signal_connect(bus, "disconnected", G_CALLBACK(disconnected), NULL);
  session_bus = g_bus_get_sync(G_BUS_TYPE_SESSION, NULL, NULL);
  if (!session_bus)
    return 0;
  char *bus_xml = g_strdup_printf(xml, NAME_BUS);
  GDBusNodeInfo *info = g_dbus_node_info_new_for_xml(bus_xml, NULL);
  g_free(bus_xml);
  g_dbus_connection_register_object(session_bus, NAME_OBJECT,
                                    info->interfaces[0], &commands, NULL, NULL,
                                    NULL);
  g_dbus_node_info_unref(info);
  owner = g_bus_own_name_on_connection(session_bus, NAME_BUS,
                                       G_BUS_NAME_OWNER_FLAGS_NONE, NULL, NULL,
                                       NULL, NULL);
  g_dbus_connection_signal_subscribe(
      session_bus, "org.gnome.ScreenSaver", "org.gnome.ScreenSaver",
      "ActiveChanged", "/org/gnome/ScreenSaver", NULL, G_DBUS_SIGNAL_FLAGS_NONE,
      screen_locked, NULL, NULL);
  GVariant *lock = g_dbus_connection_call_sync(
      session_bus, "org.gnome.ScreenSaver", "/org/gnome/ScreenSaver",
      "org.gnome.ScreenSaver", "GetActive", NULL, G_VARIANT_TYPE("(b)"),
      G_DBUS_CALL_FLAGS_NONE, 1000, NULL, NULL);
  if (lock) {
    gboolean locked;
    g_variant_get(lock, "(b)", &locked);
    goLocked(locked);
    g_variant_unref(lock);
  }
  if (keys && !goPublicBuild()) {
    keyboards = g_hash_table_new(g_str_hash, g_str_equal);
    scan_keys(NULL);
    g_timeout_add_seconds(5, scan_keys, NULL);
  }
  native_space(0);
  return 1;
}
char *native_control(const char *method) {
  GDBusConnection *c = g_bus_get_sync(G_BUS_TYPE_SESSION, NULL, NULL);
  if (!c)
    return g_strdup("{\"phase\":\"stopped\"}");
  GDBusCallFlags flags = (!strcmp(method, "Status") ||
                          !strcmp(method, "Stop") || !strcmp(method, "Cancel") || (goPublicBuild() && !strcmp(method, "Reload")))
                             ? G_DBUS_CALL_FLAGS_NO_AUTO_START
                             : G_DBUS_CALL_FLAGS_NONE;
  GVariant *v = g_dbus_connection_call_sync(
      c, NAME_BUS, NAME_OBJECT,
      NAME_BUS, method, NULL, G_VARIANT_TYPE("(s)"), flags,
      10000, NULL, NULL);
  g_object_unref(c);
  if (!v)
    return g_strdup("{\"phase\":\"stopped\"}");
  const char *s;
  g_variant_get(v, "(&s)", &s);
  char *result = g_strdup(s);
  g_variant_unref(v);
  return result;
}
int native_gui_init(void) {
  if (!gui)
    gui = gtk_init_check(NULL, NULL);
  return gui;
}
void native_clipboard(const char *text) {
  if (native_gui_init()) {
    GtkClipboard *c = gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
    gtk_clipboard_set_text(c, text, -1);
    gtk_clipboard_store(c);
  }
}

static guint notification_id = 0;
static void notified(GObject *o, GAsyncResult *result, gpointer unused) {
  GError *e = NULL;
  GVariant *v = g_dbus_connection_call_finish(G_DBUS_CONNECTION(o), result, &e);
  if (v) {
    g_variant_get(v, "(u)", &notification_id);
    g_variant_unref(v);
  }
  g_clear_error(&e);
}
void native_notify(const char *title, const char *body) {
  if (!session_bus)
    return;
  GVariantBuilder actions, hints;
  g_variant_builder_init(&actions, G_VARIANT_TYPE("as"));
  g_variant_builder_init(&hints, G_VARIANT_TYPE("a{sv}"));
  g_dbus_connection_call(session_bus, "org.freedesktop.Notifications",
                         "/org/freedesktop/Notifications",
                         "org.freedesktop.Notifications", "Notify",
                         g_variant_new("(susss@as@a{sv}i)", "思玄语音",
                                       notification_id,
                                       "audio-input-microphone", title, body,
                                       g_variant_builder_end(&actions),
                                       g_variant_builder_end(&hints), 4000),
                         G_VARIANT_TYPE("(u)"), G_DBUS_CALL_FLAGS_NONE, 2000,
                         NULL, notified, NULL);
}

void native_enable_pinyin(int select) {
  GSettings *settings = g_settings_new("org.gnome.desktop.input-sources");
  GVariant *existing = g_settings_get_value(settings, "sources");
  GVariantIter iterator;
  g_variant_iter_init(&iterator, existing);
  const char *type, *name;
  gboolean found = FALSE;
  GVariantBuilder builder;
  g_variant_builder_init(&builder, G_VARIANT_TYPE("a(ss)"));
  while (g_variant_iter_next(&iterator, "(&s&s)", &type, &name)) {
    g_variant_builder_add(&builder, "(ss)", type, name);
    if (!strcmp(type, "ibus") && !strcmp(name, NAME_PINYIN))
      found = TRUE;
  }
  if (!found) {
    g_variant_builder_add(&builder, "(ss)", "ibus", NAME_PINYIN);
    g_settings_set_value(settings, "sources", g_variant_builder_end(&builder));
  } else
    g_variant_builder_clear(&builder);
  g_variant_unref(existing);
  g_object_unref(settings);
  g_settings_sync();
  if (select)
    ibus_bus_set_global_engine_async(bus, NAME_PINYIN, 3000, NULL,
                                     NULL, NULL);
}
