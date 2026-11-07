#include "theme.h"
#include <string.h>
static GKeyFile *tokens;
static char *settings_data, *overlay_data;
static GtkCssProvider *settings_provider, *overlay_provider;
static void load_provider(GtkCssProvider **provider, const char *data) {
  if (!data || !gdk_screen_get_default()) return;
  if (!*provider) {
    *provider=gtk_css_provider_new();
    gtk_style_context_add_provider_for_screen(gdk_screen_get_default(), GTK_STYLE_PROVIDER(*provider), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION+1);
  }
  GError *error=NULL;
  if (!gtk_css_provider_load_from_data(*provider,data,-1,&error)) {
    g_warning("Appearance CSS: %s", error->message);g_error_free(error);
  }
}
void native_theme_ensure(void) {
  load_provider(&settings_provider,settings_data);
  load_provider(&overlay_provider,overlay_data);
}
void native_theme_set(const char *data,const char *settings,const char *overlay) {
  GKeyFile *next=g_key_file_new();
  if (!g_key_file_load_from_data(next,data,-1,G_KEY_FILE_NONE,NULL)) {g_key_file_unref(next);return;}
  if (tokens) g_key_file_unref(tokens);
  tokens=next;
  g_free(settings_data);g_free(overlay_data);
  settings_data=g_strdup(settings);overlay_data=g_strdup(overlay);
  native_theme_ensure();
  native_theme_previews_changed();
}
char *theme_string(const char *key) {
  return tokens?g_key_file_get_string(tokens,"theme",key,NULL):NULL;
}
double theme_number(const char *key) {
  return tokens?g_key_file_get_double(tokens,"theme",key,NULL):0;
}
GdkRGBA theme_color(const char *key) {
  GdkRGBA c={0};char *s=theme_string(key);
  if (s) {gdk_rgba_parse(&c,s);g_free(s);}
  return c;
}
