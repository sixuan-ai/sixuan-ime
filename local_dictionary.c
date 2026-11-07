#include "native.h"
#include <rime_api.h>
#include <glib.h>
#include <sys/stat.h>
#include <string.h>

// A short-lived compiler process only. Never called by the desktop daemon.
int native_dictionary_build(const char *source, const char *user, const char *schema,
                            const char *probe_code, const char *probe_text) {
  if (mkdir(user, 0700)) return 0;
  RimeApi *api = rime_get_api();
  RIME_STRUCT(RimeTraits, traits);
  traits.shared_data_dir = source;
  traits.user_data_dir = user;
  traits.app_name = "rime.sixuan-dictionary-compiler";
  traits.min_log_level = 2;
  traits.log_dir = "";
  const char *modules[] = {"default", "deployer", NULL};
  traits.modules = modules;
  api->setup(&traits);
  api->initialize(&traits);
  char *path = g_strdup_printf("%s/%s.schema.yaml", source, schema);
  int ok = api->deploy_schema(path);
  g_free(path);
  RimeSessionId session = ok ? api->create_session() : 0;
  ok = session && api->select_schema(session, schema);
  if (ok) {
    api->set_option(session, "ascii_mode", False);
    ok = api->simulate_key_sequence(session, probe_code);
    RimeCandidateListIterator iterator = {0};
    int found = 0;
    if (ok && api->candidate_list_begin(session, &iterator)) {
      for (int i = 0; i < 200 && api->candidate_list_next(&iterator); i++)
        if (!strcmp(iterator.candidate.text, probe_text)) { found = 1; break; }
      api->candidate_list_end(&iterator);
    }
    ok = found;
  }
  if (session) api->destroy_session(session);
  api->finalize();
  return ok;
}
