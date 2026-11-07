/* No display, service, or bus connection is needed for these settings paths. */
#include "../desktop.c"

char *goBuildName(int index) {
  return g_strdup(index == 1 ? "test-pinyin" : "");
}

int main(void) {
  g_assert_null(g_settings_schema_source_get_default());
  native_space(0);
  native_space(1);
  native_enable_pinyin(0);
  return 0;
}
