#include "native.h"
#include <ibus.h>
#include <rime_api.h>
#include <string.h>
extern void goPinyinChanged(uint64_t, uint64_t, char *, char *);
extern void goPinyinLearn(char *, char *);
extern int goPinyinEnabled(void);
extern char *goPinyinSchema(void);
extern int goPreferenceValue(char *);
extern char *goSetPreference(char *, int);
static void properties(IBusEngine *engine);
#define PINYIN NAME_PINYIN
typedef struct {
  char *text, *source;
  int index;
} Choice;
typedef struct {
  IBusEngine parent;
  RimeSessionId session;
  uint64_t id, revision;
  int focused, enabled, purpose, cursor, navigated;
  guint shift_held, modifiers;
  int shift_alone;
  char *raw, *preedit;
  int caret;
  GPtrArray *local, *personal, *cloud, *merged;
} PinyinEngine;
typedef struct {
  IBusEngineClass parent;
} PinyinEngineClass;
G_DEFINE_TYPE(PinyinEngine, pinyin_engine, IBUS_TYPE_ENGINE)
static RimeApi *rime;
static GHashTable *engines;
static uint64_t serial = 0;
static char *rime_dir;
static int input_english;
// Opt-in bounded diagnostics: lengths and event types only, never input text.
typedef struct {
  gint64 us;
  const char *event, *key;
  uint64_t id, revision;
  size_t raw, shown, caret;
  int focused, enabled, handled;
  guint modifiers;
} PinyinTrace;
static PinyinTrace trace_ring[96];
static unsigned trace_total;
static int trace_enabled;
static const char *trace_key(guint keyval) {
  if (keyval == IBUS_BackSpace) return "backspace";
  if (keyval == IBUS_Delete) return "delete";
  if (keyval == IBUS_space) return "space";
  if (keyval == IBUS_Return) return "enter";
  if (keyval == IBUS_Shift_L || keyval == IBUS_Shift_R) return "shift";
  if (keyval < 128 && g_ascii_isalpha(keyval)) return "letter";
  return "other";
}
static void trace_event(PinyinEngine *e, const char *event, const char *key, int handled) {
  if (!trace_enabled || e->purpose == IBUS_INPUT_PURPOSE_PASSWORD || e->purpose == IBUS_INPUT_PURPOSE_PIN) return;
  const char *raw=e->session?rime->get_input(e->session):"";
  PinyinTrace *t=&trace_ring[trace_total++ % G_N_ELEMENTS(trace_ring)];
  *t=(PinyinTrace){g_get_monotonic_time(),event,key,e->id,e->revision,
      raw?strlen(raw):0,e->preedit?strlen(e->preedit):0,
      e->session?rime->get_caret_pos(e->session):0,e->focused,e->enabled,handled,e->modifiers};
}
char *native_pinyin_trace(int action) {
  if(action==1) {trace_total=0;trace_enabled=1;}
  if(action==2) trace_enabled=0;
  GString *out=g_string_new(trace_enabled?"{\"enabled\":true,\"events\":[":"{\"enabled\":false,\"events\":[");
  unsigned first=trace_total>G_N_ELEMENTS(trace_ring)?trace_total-G_N_ELEMENTS(trace_ring):0;
  for(unsigned i=first;i<trace_total;i++) {
    PinyinTrace *t=&trace_ring[i % G_N_ELEMENTS(trace_ring)];
    g_string_append_printf(out,"%s{\"us\":%lld,\"event\":\"%s\",\"key\":\"%s\",\"engine\":%llu,\"revision\":%llu,\"raw_bytes\":%zu,\"shown_bytes\":%zu,\"caret\":%zu,\"focused\":%d,\"active\":%d,\"handled\":%d,\"modifiers\":%u}",
       i==first?"":",",(long long)t->us,t->event,t->key,(unsigned long long)t->id,(unsigned long long)t->revision,t->raw,t->shown,t->caret,t->focused,t->enabled,t->handled,t->modifiers);
  }
  g_string_append(out,"]}");return g_string_free(out,FALSE);
}
static void free_choice(gpointer data) {
  Choice *c = data;
  g_free(c->text);
  g_free(c->source);
  g_free(c);
}
static Choice *choice(const char *text, const char *source, int index) {
  Choice *c = g_new0(Choice, 1);
  c->text = g_strdup(text);
  c->source = g_strdup(source ? source : "");
  c->index = index;
  return c;
}
static int rime_init(void) {
  if (rime)
    return 1;
  rime = rime_get_api();
  rime_dir = g_build_filename(g_get_user_state_dir(), NAME_STATE,
                              "rime", NULL);
  g_mkdir_with_parents(rime_dir, 0700);
  char *defaults = g_build_filename(rime_dir, "default.yaml", NULL);
  if (!g_file_test(defaults, G_FILE_TEST_EXISTS)) {
    char *data = NULL;
    gsize length = 0;
    if (g_file_get_contents("/usr/share/rime-data/default.yaml", &data, &length,
                            NULL)) {
      g_file_set_contents(defaults, data, length, NULL);
      g_free(data);
    }
  }
  g_free(defaults);
  RIME_STRUCT(RimeTraits, traits);
  traits.shared_data_dir = "/usr/share/rime-data";
  traits.prebuilt_data_dir = "/usr/share/rime-data/build";
  traits.user_data_dir = rime_dir;
  traits.distribution_name = NAME_RIME_DISTRIBUTION;
  traits.distribution_code_name = NAME_RIME_CODE;
  traits.distribution_version = NAME_RIME_VERSION;
  traits.app_name = NAME_RIME_APP;
  static const char *modules[] = {"default", "deployer", NULL};
  traits.modules = modules;
  traits.min_log_level = 2;
  traits.log_dir = "";
  rime->setup(&traits);
  rime->initialize(&traits);
  rime->deploy_config_file("default.yaml", "config_version");
  return 1;
}
// Keep the current composition intact; dictionary changes apply when empty.
static int ensure_session_schema(RimeSessionId session) {
  const char *raw = rime->get_input(session);
  if (raw && *raw) return 0;
  char *desired = goPinyinSchema();
  RIME_STRUCT(RimeStatus, status);
  int same = rime->get_status(session, &status) && status.schema_id && !strcmp(status.schema_id, desired);
  rime->free_status(&status);
  if (same) { free(desired); return 0; }
  int selected = rime->select_schema(session, desired);
  free(desired);
  if (!selected) selected = rime->select_schema(session, "luna_pinyin_simp");
  rime->set_option(session, "ascii_mode", input_english);
  return selected;
}
static RimeSessionId create_session(void) {
  if (!rime_init())
    return 0;
  RimeSessionId session = rime->create_session();
  if (session) {
    ensure_session_schema(session);
    RIME_STRUCT(RimeStatus, status);
    int valid = rime->get_status(session, &status) && status.schema_id &&
                *status.schema_id && strcmp(status.schema_id, ".default");
    rime->free_status(&status);
    if (!valid) { rime->destroy_session(session); session = 0; }
  }
  if (session)
    rime->set_option(session, "ascii_mode", input_english);
  return session;
}
static PinyinEngine *find_engine(uint64_t id, uint64_t revision) {
  PinyinEngine *e =
      engines ? g_hash_table_lookup(engines, (gpointer)(uintptr_t)id) : NULL;
  return e && e->revision == revision && e->focused && e->enabled &&
                 e->purpose != IBUS_INPUT_PURPOSE_PASSWORD &&
                 e->purpose != IBUS_INPUT_PURPOSE_PIN
             ? e
             : NULL;
}
static void merge_one(PinyinEngine *e, Choice *c) {
  for (guint i = 0; i < e->merged->len; i++)
    if (!strcmp(((Choice *)g_ptr_array_index(e->merged, i))->text, c->text))
      return;
  g_ptr_array_add(e->merged, c);
}
static void show_lookup(PinyinEngine *e) {
  g_ptr_array_set_size(e->merged, 0);
  for (guint i = 0; i < MIN(3, e->personal->len); i++)
    merge_one(e, g_ptr_array_index(e->personal, i));
  for (guint i = 0; i < MIN(5, e->local->len); i++)
    merge_one(e, g_ptr_array_index(e->local, i));
  for (guint i = 0; i < e->cloud->len; i++)
    merge_one(e, g_ptr_array_index(e->cloud, i));
  for (guint i = 5; i < e->local->len; i++)
    merge_one(e, g_ptr_array_index(e->local, i));
  for (guint i = 3; i < e->personal->len; i++)
    merge_one(e, g_ptr_array_index(e->personal, i));
  IBusLookupTable *table = ibus_lookup_table_new(9, e->cursor, TRUE, TRUE);
  for (guint i = 0; i < e->merged->len; i++) {
    Choice *c = g_ptr_array_index(e->merged, i);
    char *label = *c->source ? g_strdup_printf("%s 〔%s〕", c->text, c->source)
                             : g_strdup(c->text);
    ibus_lookup_table_append_candidate(table, ibus_text_new_from_string(label));
    g_free(label);
  }
  if (e->merged->len)
    e->cursor = CLAMP(e->cursor, 0, (int)e->merged->len - 1);
  else
    e->cursor = 0;
  ibus_lookup_table_set_cursor_pos(table, e->cursor);
  ibus_engine_update_lookup_table(IBUS_ENGINE(e), table, e->merged->len > 0);
}
static void show(PinyinEngine *e) {
  show_lookup(e);
  const char *text = e->preedit ? e->preedit : "";
  IBusText *preedit = ibus_text_new_from_string(text);
  ibus_text_append_attribute(preedit, IBUS_ATTR_TYPE_UNDERLINE,
                             IBUS_ATTR_UNDERLINE_SINGLE, 0,
                             g_utf8_strlen(text, -1));
  ibus_engine_update_preedit_text_with_mode(
      IBUS_ENGINE(e), preedit, MIN(e->caret, g_utf8_strlen(text, -1)),
      *text != 0, IBUS_ENGINE_PREEDIT_CLEAR);
}
static void render(PinyinEngine *e) {
  if (!e->session)
    return;
  g_free(e->raw);
  e->raw = g_strdup(rime->get_input(e->session));
  g_free(e->preedit);
  e->preedit = g_strdup("");
  e->caret = 0;
  int english = rime->get_option(e->session, "ascii_mode");
  if (e->focused && e->enabled && english != input_english) {
    input_english = english;
    properties(IBUS_ENGINE(e));
  }
  e->revision++;
  e->cursor = 0;
  e->navigated = 0;
  g_ptr_array_set_size(e->local, 0);
  g_ptr_array_set_size(e->personal, 0);
  g_ptr_array_set_size(e->cloud, 0);
  RIME_STRUCT(RimeContext, ctx);
  if (rime->get_context(e->session, &ctx)) {
    g_free(e->preedit);
    e->preedit =
        g_strdup(ctx.composition.preedit ? ctx.composition.preedit : "");
    e->caret = ctx.composition.cursor_pos;
    rime->free_context(&ctx);
  }
  RimeCandidateListIterator iterator;
  if (rime->candidate_list_begin(e->session, &iterator)) {
    int index = 0;
    while (index < 50 && rime->candidate_list_next(&iterator)) {
      g_ptr_array_add(e->local, choice(iterator.candidate.text, "", index++));
    }
    rime->candidate_list_end(&iterator);
  }
  goPinyinChanged(e->id, e->revision, e->raw, e->preedit);
  show(e);
  trace_event(e,"render","none",-1);
}
static void clear(PinyinEngine *e) {
  if (e->session)
    rime->clear_composition(e->session);
  render(e);
}
static int collect_commit(PinyinEngine *e, const char *before) {
  RIME_STRUCT(RimeCommit, commit);
  if (rime->get_commit(e->session, &commit)) {
    ibus_engine_commit_text(IBUS_ENGINE(e),
                            ibus_text_new_from_string(commit.text));
    const char *remaining = rime->get_input(e->session);
    if (before && *before && (!remaining || !*remaining))
      goPinyinLearn((char *)before, commit.text);
    rime->free_commit(&commit);
    return 1;
  }
  return 0;
}
static void commit_partial(PinyinEngine *e) {
  const char *raw = rime->get_input(e->session);
  if (!raw || !*raw || rime->get_caret_pos(e->session) != strlen(raw) ||
      !RIME_API_AVAILABLE(rime, set_input))
    return;
  RIME_STRUCT(RimeContext, ctx);
  if (!rime->get_context(e->session, &ctx))
    return;
  const char *text = ctx.composition.preedit;
  int start = ctx.composition.sel_start;
  int count = 0;
  if (!text || start <= 0 || start >= (int)strlen(text) ||
      !g_utf8_validate(text, -1, NULL) || !g_utf8_validate(text, start, NULL))
    goto done;
  for (int i = 0; i < start; i++)
    if (g_ascii_isalpha(text[i]))
      goto done;
  for (const char *p = text + start; *p; p = g_utf8_next_char(p)) {
    gunichar ch = g_utf8_get_char(p);
    if (ch == ' ' || ch == '\'')
      continue;
    if (!(ch < 128 && g_ascii_isalpha(ch)) && ch != 0xfc)
      goto done;
    count++;
  }
  if (!count)
    goto done;
  const char *suffix = raw + strlen(raw);
  while (suffix > raw && count) {
    char ch = *--suffix;
    if (g_ascii_isalpha(ch))
      count--;
    else if (ch != '\'')
      goto done;
  }
  if (count || suffix == raw)
    goto done;
  const char *end = suffix;
  while (end > raw && end[-1] == '\'')
    end--;
  while (*suffix == '\'')
    suffix++;
  char *confirmed = g_strndup(text, start);
  char *prefix = g_strndup(raw, end - raw);
  char *remaining = g_strdup(suffix);
  ibus_engine_commit_text(IBUS_ENGINE(e), ibus_text_new_from_string(confirmed));
  rime->set_input(e->session, remaining);
  goPinyinLearn(prefix, confirmed);
  g_free(remaining);
  g_free(prefix);
  g_free(confirmed);
done:
  rime->free_context(&ctx);
}
static void choose(PinyinEngine *e, int index) {
  if (index < 0 || index >= (int)e->merged->len)
    return;
  Choice *c = g_ptr_array_index(e->merged, index);
  char *before = g_strdup(e->raw);
  if (c->index >= 0) {
    rime->select_candidate(e->session, c->index);
    if (!collect_commit(e, before))
      commit_partial(e);
  } else {
    char *text = g_strdup(c->text);
    ibus_engine_commit_text(IBUS_ENGINE(e), ibus_text_new_from_string(text));
    goPinyinLearn(before, text);
    rime->clear_composition(e->session);
    g_free(text);
  }
  g_free(before);
  render(e);
}
static void toggle_input_mode(PinyinEngine *e) {
  if (!e->session)
    e->session = create_session();
  if (!e->session)
    return;
  int english = !rime->get_option(e->session, "ascii_mode");
  // A deliberate switch to English preserves the unselected spelling once.
  const char *raw = rime->get_input(e->session);
  if (english && raw && *raw)
    ibus_engine_commit_text(IBUS_ENGINE(e), ibus_text_new_from_string(raw));
  rime->clear_composition(e->session);
  input_english = english;
  rime->set_option(e->session, "ascii_mode", english);
  render(e);
  properties(IBUS_ENGINE(e));
}
static int shift_key(PinyinEngine *e, guint keyval, guint state) {
  guint bit = keyval == IBUS_Shift_L ? 1 : keyval == IBUS_Shift_R ? 2 : 0;
  guint chord = state & (IBUS_CONTROL_MASK | IBUS_MOD1_MASK | IBUS_SUPER_MASK |
                         IBUS_META_MASK | IBUS_HYPER_MASK);
  if (!bit) {
    if (e->shift_held)
      e->shift_alone = 0;
    return -1;
  }
  if (state & IBUS_RELEASE_MASK) {
    int toggle = (e->shift_held & bit) && e->shift_alone && !chord;
    e->shift_held &= ~bit;
    e->shift_alone = 0;
    if (toggle) {
      toggle_input_mode(e);
      return TRUE;
    }
    return FALSE;
  }
  if (!(e->shift_held & bit)) {
    e->shift_alone = !e->shift_held && !chord;
    e->shift_held |= bit;
  }
  return FALSE;
}
static gboolean process(IBusEngine *engine, guint keyval, guint keycode,
                        guint state) {
  PinyinEngine *e = (PinyinEngine *)engine;
  e->modifiers = state;
  if (!goPinyinEnabled() || e->purpose == IBUS_INPUT_PURPOSE_PASSWORD ||
      e->purpose == IBUS_INPUT_PURPOSE_PIN) {
    e->shift_held = 0;
    e->shift_alone = 0;
    return FALSE;
  }
  if (native_public_key(keyval, keycode, state)) return TRUE;
  if (!e->session)
    e->session = create_session();
  if (!e->session)
    return FALSE;
  if (ensure_session_schema(e->session)) render(e);
  int shift = shift_key(e, keyval, state);
  if (shift >= 0) { trace_event(e,"shift","shift",shift); return shift; }
  if (state & IBUS_RELEASE_MASK)
    return FALSE;
  // IBus also supplies lock LEDs, pointer buttons and adapter flags. Rime
  // special-key matching needs only actual shortcut modifiers; Num Lock must
  // not make an ordinary Backspace look like an unsupported key combination.
  state &= IBUS_SHIFT_MASK | IBUS_CONTROL_MASK | IBUS_MOD1_MASK | IBUS_MOD4_MASK |
           IBUS_SUPER_MASK | IBUS_HYPER_MASK | IBUS_META_MASK;
  int word_delete = e->raw && *e->raw && (state & IBUS_CONTROL_MASK) &&
      !(state & (IBUS_MOD1_MASK | IBUS_SUPER_MASK)) &&
      (keyval == IBUS_BackSpace || keyval == IBUS_Delete);
  if ((state & (IBUS_CONTROL_MASK | IBUS_MOD1_MASK | IBUS_SUPER_MASK)) && !word_delete) {
    trace_event(e,"passthrough_modifier",trace_key(keyval),0);
    return FALSE;
  }
  if (rime->get_option(e->session, "ascii_mode")) {
    trace_event(e,"english_passthrough",trace_key(keyval),0);
    return FALSE;
  }
  if (e->raw && *e->raw) {
    if (keyval == IBUS_Escape) {
      clear(e);
      return TRUE;
    }
    if (keyval == IBUS_space) {
      choose(e, e->cursor);
      return TRUE;
    }
    if (keyval >= '1' && keyval <= '9') {
      choose(e, (e->cursor / 9) * 9 + keyval - '1');
      return TRUE;
    }
    int delta = 0;
    if (keyval == IBUS_Up)
      delta = -1;
    else if (keyval == IBUS_Down)
      delta = 1;
    else if (keyval == IBUS_Page_Up)
      delta = -9;
    else if (keyval == IBUS_Page_Down)
      delta = 9;
    if (delta) {
      e->navigated = 1;
      e->revision++;
      e->cursor = CLAMP(e->cursor + delta, 0, MAX(0, (int)e->merged->len - 1));
      goPinyinChanged(e->id, e->revision, "", "");
      show_lookup(e);
      return TRUE;
    }
  }
  trace_event(e,"key_before",trace_key(keyval),-1);
  char *before = g_strdup(e->raw);
  int handled;
  if (word_delete && keyval == IBUS_Delete) {
    size_t caret = rime->get_caret_pos(e->session);
    // Rime's navigator knows syllable boundaries; avoid guessing from letters.
    rime->process_key(e->session, IBUS_Right, 0);
    size_t end = rime->get_caret_pos(e->session);
    rime->set_caret_pos(e->session, caret);
    while (end > caret) {
      if (!rime->process_key(e->session, IBUS_Delete, 0)) break;
      end--;
    }
    handled = TRUE;
  } else {
    handled = rime->process_key(e->session, keyval, state);
  }
  // Never let the editor independently delete a live composition word. At a
  // boundary Rime may decline the key; the composition must still own it.
  if (word_delete && !handled) handled = TRUE;
  collect_commit(e, before);
  g_free(before);
  if (handled)
    render(e);
  trace_event(e,"key_after",trace_key(keyval),handled);
  return handled;
}
static void focus_in(IBusEngine *engine) {
  PinyinEngine *e = (PinyinEngine *)engine;
  trace_event(e,"focus_in","none",-1);
  e->focused = 1;
  if (goPinyinEnabled() && !e->session)
    e->session = create_session();
  if (e->session)
    rime->set_option(e->session, "ascii_mode", input_english);
  properties(engine);
}
static void focus_in_id(IBusEngine *e, const char *path, const char *client) {
  focus_in(e);
}
static void reset(IBusEngine *engine) {
  PinyinEngine *e = (PinyinEngine *)engine;
  trace_event(e,"reset","none",-1);
  e->shift_held = 0;
  e->shift_alone = 0;
  clear(e);
}
static void focus_out(IBusEngine *engine) {
  PinyinEngine *e = (PinyinEngine *)engine;
  trace_event(e,"focus_out","none",-1);
  e->focused = 0;
  e->shift_held = 0;
  e->shift_alone = 0;
  clear(e);
}
static void focus_out_id(IBusEngine *e, const char *path) { focus_out(e); }
static void enable(IBusEngine *engine) {
  PinyinEngine *e = (PinyinEngine *)engine;
  e->enabled = 1;
  properties(engine);
}
static void disable(IBusEngine *engine) {
  PinyinEngine *e = (PinyinEngine *)engine;
  e->enabled = 0;
  e->shift_held = 0;
  e->shift_alone = 0;
  clear(e);
}
static void content(IBusEngine *engine, guint purpose, guint hints) {
  PinyinEngine *e = (PinyinEngine *)engine;
  e->purpose = purpose;
  if (purpose == IBUS_INPUT_PURPOSE_PASSWORD ||
      purpose == IBUS_INPUT_PURPOSE_PIN) {
    e->shift_held = 0;
    e->shift_alone = 0;
    clear(e);
  }
}
static void clicked(IBusEngine *engine, guint index, guint button,
                    guint state) {
  PinyinEngine *e = (PinyinEngine *)engine;
  if (button == 1)
    choose(e, (e->cursor / 9) * 9 + index);
}
static void destroy(IBusObject *object) {
  PinyinEngine *e = (PinyinEngine *)object;
  e->focused = 0;
  e->revision++;
  goPinyinChanged(e->id, e->revision, "", "");
  if (e->session)
    rime->destroy_session(e->session);
  if (engines)
    g_hash_table_remove(engines, (gpointer)(uintptr_t)e->id);
  g_free(e->raw);
  g_free(e->preedit);
  g_ptr_array_free(e->merged, TRUE);
  g_ptr_array_free(e->local, TRUE);
  g_ptr_array_free(e->personal, TRUE);
  g_ptr_array_free(e->cloud, TRUE);
  IBUS_OBJECT_CLASS(pinyin_engine_parent_class)->destroy(object);
}
static IBusProperty *property(const char *key, IBusPropType type,
                              const char *label, int checked,
                              IBusPropList *children) {
  return ibus_property_new(key, type, ibus_text_new_from_string(label), "",
                           ibus_text_new_from_string(label), TRUE, TRUE,
                           checked ? PROP_STATE_CHECKED : PROP_STATE_UNCHECKED,
                           children);
}
static void properties(IBusEngine *engine) {
  IBusPropList *list = ibus_prop_list_new(), *modes = ibus_prop_list_new();
  IBusProperty *mode = property("InputMode", PROP_TYPE_NORMAL,
                                input_english ? "英文模式（Shift 切换）"
                                              : "中文模式（Shift 切换）",
                                0, NULL);
  ibus_property_set_symbol(
      mode, ibus_text_new_from_string(input_english ? "英" : "中"));
  ibus_prop_list_append(list, mode);
  int selected = goPreferenceValue("text_polish");
  const char *names[] = {"关闭（原始识别）", "基础整理", "增强整理"};
  const char *keys[] = {"PolishOff", "PolishBasic", "PolishAggressive"};
  for (int i = 0; i < 3; i++)
    ibus_prop_list_append(modes, property(keys[i], PROP_TYPE_RADIO, names[i],
                                          selected == i, NULL));
  char *title =
      g_strdup_printf("语音文字整理：%s", names[CLAMP(selected, 0, 2)]);
  ibus_prop_list_append(
      list, property("TextPolish", PROP_TYPE_MENU, title, 0, modes));
  g_free(title);
  ibus_prop_list_append(list, property("LocalDictionary", PROP_TYPE_TOGGLE, "扩展词库", goPreferenceValue("local_dictionary"), NULL));
  ibus_prop_list_append(
      list, property("CloudCandidates", PROP_TYPE_TOGGLE, "云端候选",
                     goPreferenceValue("cloud_candidates"), NULL));
  ibus_prop_list_append(
      list, property("SoundFeedback", PROP_TYPE_TOGGLE, "录音提示音",
                     goPreferenceValue("sound_feedback"), NULL));
  ibus_prop_list_append(list,
                        property("PinyinEnabled", PROP_TYPE_TOGGLE, "拼音输入",
                                 goPreferenceValue("pinyin_enabled"), NULL));
  ibus_prop_list_append(list, property("Preferences", PROP_TYPE_NORMAL,
                                       NAME_PREFERENCES, 0, NULL));
  ibus_engine_register_properties(engine, list);
}
static void property_activate(IBusEngine *engine, const gchar *name,
                              guint state) {
  if (!strcmp(name, "InputMode")) {
    toggle_input_mode((PinyinEngine *)engine);
    return;
  }
  if (!strcmp(name, "Preferences")) {
    native_settings_show();
    return;
  }
  const char *key = NULL;
  int value = state == PROP_STATE_CHECKED;
  if (!strcmp(name, "LocalDictionary"))
    key = "local_dictionary";
  else if (!strcmp(name, "CloudCandidates"))
    key = "cloud_candidates";
  else if (!strcmp(name, "SoundFeedback"))
    key = "sound_feedback";
  else if (!strcmp(name, "PinyinEnabled"))
    key = "pinyin_enabled";
  else if (state == PROP_STATE_CHECKED) {
    if (!strcmp(name, "PolishOff")) {
      key = "text_polish";
      value = 0;
    } else if (!strcmp(name, "PolishBasic")) {
      key = "text_polish";
      value = 1;
    } else if (!strcmp(name, "PolishAggressive")) {
      key = "text_polish";
      value = 2;
    }
  }
  if (key) {
    char *error = goSetPreference((char *)key, value);
    if (*error)
      native_notify("设置未保存", error);
    free(error);
  }
}
void native_pinyin_preferences_changed(void) {
  if (!engines)
    return;
  GHashTableIter iterator;
  gpointer item;
  g_hash_table_iter_init(&iterator, engines);
  while (g_hash_table_iter_next(&iterator, NULL, &item)) {
    PinyinEngine *e = item;
    if (e->session && ensure_session_schema(e->session)) render(e);
    if (!goPinyinEnabled())
      clear(e);
    else if (!goPreferenceValue("cloud_candidates")) {
      g_ptr_array_set_size(e->cloud, 0);
      if (e->focused && e->enabled)
        show_lookup(e);
    }
    if (e->focused && e->enabled)
      properties(IBUS_ENGINE(e));
  }
}
static void pinyin_engine_class_init(PinyinEngineClass *klass) {
  IBusEngineClass *e = IBUS_ENGINE_CLASS(klass);
  e->process_key_event = process;
  e->reset = reset;
  e->focus_in = focus_in;
  e->focus_out = focus_out;
  e->focus_in_id = focus_in_id;
  e->focus_out_id = focus_out_id;
  e->enable = enable;
  e->disable = disable;
  e->set_content_type = content;
  e->candidate_clicked = clicked;
  e->property_activate = property_activate;
  IBUS_OBJECT_CLASS(klass)->destroy = destroy;
}
static void pinyin_engine_init(PinyinEngine *e) {
  e->id = ++serial;
  if (!engines)
    engines = g_hash_table_new(g_direct_hash, g_direct_equal);
  g_hash_table_insert(engines, (gpointer)(uintptr_t)e->id, e);
  e->local = g_ptr_array_new_with_free_func(free_choice);
  e->personal = g_ptr_array_new_with_free_func(free_choice);
  e->cloud = g_ptr_array_new_with_free_func(free_choice);
  e->merged = g_ptr_array_new();
  e->raw = g_strdup("");
  e->preedit = g_strdup("");
}
uintptr_t native_pinyin_type(void) { return pinyin_engine_get_type(); }
void native_pinyin_add(uint64_t id, uint64_t revision, const char *text,
                       const char *source, int cloud) {
  PinyinEngine *e = find_engine(id, revision);
  if (!e || e->navigated)
    return;
  GPtrArray *items = cloud ? e->cloud : e->personal;
  if (items->len >= 10)
    return;
  g_ptr_array_add(items, choice(text, source, -1));
}
void native_pinyin_refresh(uint64_t id, uint64_t revision) {
  PinyinEngine *e = find_engine(id, revision);
  if (e && !e->navigated)
    show_lookup(e);
}
char *native_rime_query(const char *input) {
  RimeSessionId session = create_session();
  if (!session)
    return g_strdup("");
  for (const char *p = input; *p; p++)
    rime->process_key(session, *p, 0);
  RIME_STRUCT(RimeContext, ctx);
  GString *result = g_string_new("");
  if (rime->get_context(session, &ctx)) {
    for (int i = 0; i < ctx.menu.num_candidates; i++) {
      g_string_append(result, ctx.menu.candidates[i].text);
      g_string_append_c(result, '\n');
    }
    rime->free_context(&ctx);
  }
  rime->destroy_session(session);
  return g_string_free(result, FALSE);
}

char *native_pinyin_schema_status(void) {
  if (rime && engines) {
    GHashTableIter iterator; gpointer item;
    g_hash_table_iter_init(&iterator, engines);
    while (g_hash_table_iter_next(&iterator, NULL, &item)) {
      PinyinEngine *e = item;
      if (!e->focused || !e->enabled || !e->session) continue;
      RIME_STRUCT(RimeStatus, status);
      if (rime->get_status(e->session, &status)) {
        char *result = g_strdup(status.schema_id ? status.schema_id : "");
        rime->free_status(&status);return result;
      }
    }
  }
  return g_strdup("");
}
