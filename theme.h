#ifndef SIXUAN_THEME_H
#define SIXUAN_THEME_H
#include <gtk/gtk.h>
void native_theme_set(const char *, const char *, const char *);
void native_theme_ensure(void);
GdkRGBA theme_color(const char *);
double theme_number(const char *);
char *theme_string(const char *);
GtkWidget *native_overlay_preview(void);
GtkWidget *native_candidate_preview(void);
void native_theme_previews_changed(void);
#endif
