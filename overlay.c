#include "native.h"
#include <gtk/gtk.h>
#include <math.h>
#include <string.h>
#include "theme.h"
extern void goPosition(int, int);
static GtkWidget *window, *clock_label, *meter_label, *drawing;
static guint timer;
static GtkWidget *dot, *card;
/* 0: idle, 1: live, 2: quiet, 3: clipping; visual state only. */
static int visual_state;
/* Saved coordinates describe the visible card, excluding shadow padding. */
enum { SHADOW_MARGIN = 24 };
static double start_time, last_level, dbfs, history[32];
static int x_position, y_position, dragging, clip, received;
static double drag_root_x, drag_root_y;
static int drag_x, drag_y;
static void clamp_position(int *x, int *y) {
  GtkAllocation a;
  gtk_widget_get_allocation(window, &a);
  GdkDisplay *display = gtk_widget_get_display(window);
  int card_width = MAX(1, a.width - 2 * SHADOW_MARGIN);
  int card_height = MAX(1, a.height - 2 * SHADOW_MARGIN);
  GdkMonitor *m = gdk_display_get_monitor_at_point(
      display, *x + card_width / 2, *y + card_height / 2);
  GdkRectangle area;
  gdk_monitor_get_workarea(m, &area);
  *x = CLAMP(*x, area.x, area.x + MAX(0, area.width - card_width));
  *y = CLAMP(*y, area.y, area.y + MAX(0, area.height - card_height));
}
static void position(GtkWidget *widget, GtkAllocation *a, gpointer unused) {
  if (x_position == -99999) {
    GdkDisplay *d = gtk_widget_get_display(widget);
    GdkDevice *pointer = gdk_seat_get_pointer(gdk_display_get_default_seat(d));
    gint px, py;
    gdk_device_get_position(pointer, NULL, &px, &py);
    GdkRectangle area;
    gdk_monitor_get_workarea(gdk_display_get_monitor_at_point(d, px, py),
                             &area);
    x_position = area.x + (area.width - (a->width - 2 * SHADOW_MARGIN)) / 2;
    y_position = area.y + area.height - (a->height - 2 * SHADOW_MARGIN) - 76;
  }
  clamp_position(&x_position, &y_position);
  gtk_window_move(GTK_WINDOW(window), x_position - SHADOW_MARGIN,
                  y_position - SHADOW_MARGIN);
  /* Let clicks on the transparent shadow gutter reach windows underneath. */
  cairo_rectangle_int_t visible = {SHADOW_MARGIN, SHADOW_MARGIN,
                                   MAX(1, a->width - 2 * SHADOW_MARGIN),
                                   MAX(1, a->height - 2 * SHADOW_MARGIN)};
  cairo_region_t *region = cairo_region_create_rectangle(&visible);
  gtk_widget_input_shape_combine_region(window, region);
  cairo_region_destroy(region);
}
static gboolean press(GtkWidget *w, GdkEventButton *e, gpointer unused) {
  if (e->button != 1)
    return FALSE;
  GtkAllocation a;
  gtk_widget_get_allocation(w, &a);
  int wx, wy;
  gtk_window_get_position(GTK_WINDOW(w), &wx, &wy);
  /* Events may originate in the waveform's child GdkWindow. Root coordinates
   * keep the hit rectangle relative to the whole popup in that case too. */
  double local_x = e->x_root - wx, local_y = e->y_root - wy;
  if (local_x < SHADOW_MARGIN || local_y < SHADOW_MARGIN ||
      local_x >= a.width - SHADOW_MARGIN || local_y >= a.height - SHADOW_MARGIN)
    return FALSE;
  dragging = 1;
  drag_root_x = e->x_root;
  drag_root_y = e->y_root;
  gtk_window_get_position(GTK_WINDOW(w), &drag_x, &drag_y);
  drag_x += SHADOW_MARGIN;
  drag_y += SHADOW_MARGIN;
  return TRUE;
}
static gboolean motion(GtkWidget *w, GdkEventMotion *e, gpointer unused) {
  if (!dragging)
    return FALSE;
  x_position = drag_x + round(e->x_root - drag_root_x);
  y_position = drag_y + round(e->y_root - drag_root_y);
  clamp_position(&x_position, &y_position);
  gtk_window_move(GTK_WINDOW(window), x_position - SHADOW_MARGIN,
                  y_position - SHADOW_MARGIN);
  return TRUE;
}
static gboolean release(GtkWidget *w, GdkEventButton *e, gpointer unused) {
  if (e->button != 1 || !dragging)
    return FALSE;
  dragging = 0;
  goPosition(x_position, y_position);
  return TRUE;
}
static void realized(GtkWidget *widget, gpointer unused) {
  GdkCursor *cursor =
      gdk_cursor_new_from_name(gtk_widget_get_display(widget), "move");
  gdk_window_set_cursor(gtk_widget_get_window(widget), cursor);
  g_object_unref(cursor);
}
static void paint_wave(cairo_t *cr, int width, int height, int stale) {
  GdkRGBA accent=theme_color("accent"), hi=theme_color("wave-hi"), muted=theme_color("recording-muted");
  GdkRGBA error=theme_color("error"),error_hi=theme_color("error-hi");
  double glow=theme_number("glow_k");
  double spacing = width / 32.0;
  cairo_set_line_cap(cr, CAIRO_LINE_CAP_ROUND);
  for (int i = 0; i < 32; i++) {
    double level = stale ? 0 : history[i];
    double h = MAX(4, level * height), x = (i + .5) * spacing;
    double top = (height - h) / 2, bottom = (height + h) / 2;
    if (level > 0) {
      /* Broad translucent strokes supply the halo without a blur surface. */
      cairo_set_source_rgba(cr, clip ? error.red : accent.red,
                            clip ? error.green : accent.green,
                            clip ? error.blue : accent.blue, .12*glow);
      for (int halo = glow>0?2:0; halo >= 1; halo--) {
        cairo_set_line_width(cr, (halo == 2 ? 7.5 : 5)*glow);
        cairo_move_to(cr, x, top + 1.5);
        cairo_line_to(cr, x, bottom - 1.5);
        cairo_stroke(cr);
      }
      cairo_pattern_t *gradient = cairo_pattern_create_linear(0, top, 0, bottom);
      cairo_pattern_add_color_stop_rgb(gradient, 0, clip ? error_hi.red : hi.red,
                                       clip ? error_hi.green : hi.green,
                                       clip ? error_hi.blue : hi.blue);
      cairo_pattern_add_color_stop_rgb(gradient, 1, clip ? error.red : accent.red,
                                       clip ? error.green : accent.green,
                                       clip ? error.blue : accent.blue);
      cairo_set_source(cr, gradient);
      cairo_pattern_destroy(gradient);
    } else {
      cairo_set_source_rgba(cr, muted.red, muted.green, muted.blue, .38);
    }
    cairo_set_line_width(cr, 3);
    cairo_move_to(cr, x, top + 1.5);
    cairo_line_to(cr, x, bottom - 1.5);
    cairo_stroke(cr);
  }
}
static gboolean draw_dot(GtkWidget *widget, cairo_t *cr, gpointer unused) {
  GtkWidget *orb = unused ? unused : dot;
  int state=GPOINTER_TO_INT(g_object_get_data(G_OBJECT(widget),"preview")) ? 1 : visual_state;
  GdkRGBA accent=theme_color("accent"), hi=theme_color("orb-hi"), end=theme_color("accent-2"), muted=theme_color("recording-muted"), idle_hi=theme_color("orb-idle-hi"), light=theme_color("orb-shadow-light"), dark_color=theme_color("orb-shadow-dark");
  GdkRGBA error=theme_color("error"),error_hi=theme_color("error-orb-hi");
  double glow=theme_number("glow_k");
  GtkAllocation a;
  gtk_widget_get_allocation(orb, &a);
  int dx, dy;
  gtk_widget_translate_coordinates(orb, widget, 0, 0, &dx, &dy);
  cairo_save(cr);
  cairo_translate(cr, dx, dy);
  /* Paint the two outer shadows on the card. GTK clips a drawing area's
   * CSS outer shadows to its allocation, so these halos use the reference's
   * -3/-3 light and +4/+4 dark offsets with 7/9px falloffs directly. */
  for (int dark = 0; dark < 2; dark++) {
    double offset = dark ? 4 : -3, radius = dark ? 9 : 7;
    cairo_pattern_t *shadow = cairo_pattern_create_radial(
        22 + offset, 22 + offset, 22, 22 + offset, 22 + offset, 22 + radius);
    cairo_pattern_add_color_stop_rgba(shadow, 0, dark ? 0 : 1, dark ? 0 : 1,
                                      dark ? 0 : 1, dark ? dark_color.alpha : light.alpha);
    cairo_pattern_add_color_stop_rgba(shadow, 1, dark ? 0 : 1, dark ? 0 : 1,
                                      dark ? 0 : 1, 0);
    cairo_set_source(cr, shadow);
    cairo_arc(cr, 22 + offset, 22 + offset, 22 + radius, 0, 2 * G_PI);
    cairo_fill(cr);
    cairo_pattern_destroy(shadow);
  }
  gtk_render_background(gtk_widget_get_style_context(orb), cr, 0, 0, a.width, a.height);
  double x = a.width / 2.0, y = a.height / 2.0;
  double pulse = state == 0 ? 0 :
      .5 - .5 * cos((g_get_monotonic_time() / 1e6 - start_time) * 2 * G_PI / 1.6);
  double r = state == 0 ? muted.red : state == 3 ? error.red : accent.red;
  double g = state == 0 ? muted.green : state == 3 ? error.green : accent.green;
  double b = state == 0 ? muted.blue : state == 3 ? error.blue : accent.blue;
  cairo_pattern_t *halo = cairo_pattern_create_radial(x, y, 7, x, y, (17.5 + pulse * 3)*MAX(glow,.01));
  cairo_pattern_add_color_stop_rgba(halo, 0, r, g, b, (state == 0 ? .10 : theme_number("orb_glow_alpha"))*glow);
  cairo_pattern_add_color_stop_rgba(halo, 1, r, g, b, 0);
  cairo_set_source(cr, halo);
  cairo_arc(cr, x, y, 21*MAX(glow,1), 0, 2 * G_PI);
  cairo_fill(cr);
  cairo_pattern_destroy(halo);
  cairo_set_source_rgba(cr, r, g, b, (state == 0 ? .10 : .14 - pulse * .08)*glow);
  cairo_arc(cr, x, y, 11 + pulse * 3, 0, 2 * G_PI);
  cairo_fill(cr);
  cairo_pattern_t *core = cairo_pattern_create_radial(x - 2, y - 2, 0, x, y, 7);
  cairo_pattern_add_color_stop_rgb(core, 0, state == 3 ? error_hi.red : state == 0 ? idle_hi.red : hi.red,
                                  state == 3 ? error_hi.green : state == 0 ? idle_hi.green : hi.green,
                                  state == 3 ? error_hi.blue : state == 0 ? idle_hi.blue : hi.blue);
  cairo_pattern_add_color_stop_rgb(core, .45, r, g, b);
  cairo_pattern_add_color_stop_rgb(core, 1, state == 1 || state == 2 ? end.red : r,
                                  state == 1 || state == 2 ? end.green : g,
                                  state == 1 || state == 2 ? end.blue : b);
  cairo_set_source(cr, core);
  cairo_arc(cr, x, y, 7, 0, 2 * G_PI);
  cairo_fill(cr);
  cairo_pattern_destroy(core);
  cairo_restore(cr);
  return FALSE;
}
static gboolean draw_wave(GtkWidget *widget, cairo_t *cr, gpointer unused) {
  GtkAllocation a;
  gtk_widget_get_allocation(widget, &a);
  paint_wave(cr, a.width, a.height,
             g_get_monotonic_time() / 1e6 - last_level > .5);
  return FALSE;
}
static gboolean tick(gpointer unused) {
  if (!window)
    return G_SOURCE_REMOVE;
  double now = g_get_monotonic_time() / 1e6;
  int elapsed = MAX(0, (int)(now - start_time));
  char counter[32];
  g_snprintf(counter, sizeof(counter), "%02d:%02d", elapsed / 60, elapsed % 60);
  gtk_label_set_text(GTK_LABEL(clock_label), counter);
  const char *message = (now - last_level > .5)     ? "等待音频…"
                        : (!received || dbfs < -60) ? "等待声音…"
                        : clip                      ? "声音过大"
                        : dbfs < -45                ? "声音较轻"
                                                    : "正在收音";
  gtk_label_set_text(GTK_LABEL(meter_label), message);
  int state = now - last_level > .5 || !received || dbfs < -60 ? 0 :
              clip ? 3 : dbfs < -45 ? 2 : 1;
  if (state != visual_state) {
    GtkStyleContext *context = gtk_widget_get_style_context(card);
    gtk_style_context_remove_class(context, "idle");
    gtk_style_context_remove_class(context, "quiet");
    gtk_style_context_remove_class(context, "clip");
    if (state == 0) gtk_style_context_add_class(context, "idle");
    if (state == 2) gtk_style_context_add_class(context, "quiet");
    if (state == 3) gtk_style_context_add_class(context, "clip");
    visual_state = state;
  }
  gtk_widget_queue_draw(card);
  gtk_widget_queue_draw(drawing);
  return G_SOURCE_CONTINUE;
}
static GtkWidget *label(const char *text, const char *name) {
  GtkWidget *w = gtk_label_new(text);
  gtk_widget_set_name(w, name);
  int height = !strcmp(name, "recording-title") ? 20 :
               !strcmp(name, "recording-status") ? 16 :
               !strcmp(name, "recording-time") ? 15 : 14;
  PangoAttrList *line = pango_attr_list_new();
  pango_attr_list_insert(line, pango_attr_line_height_new_absolute(height * PANGO_SCALE));
  gtk_label_set_attributes(GTK_LABEL(w), line);
  pango_attr_list_unref(line);
  return w;
}
static GtkWidget *recording_content_new(void) {
  card = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 14);
  gtk_widget_set_name(card, "recording-card");
  gtk_widget_set_size_request(card, 496, 64);
  gtk_widget_set_margin_start(card, SHADOW_MARGIN);
  gtk_widget_set_margin_end(card, SHADOW_MARGIN);
  gtk_widget_set_margin_top(card, SHADOW_MARGIN);
  gtk_widget_set_margin_bottom(card, SHADOW_MARGIN);
  visual_state = 0;
  gtk_style_context_add_class(gtk_widget_get_style_context(card), "idle");
  dot = gtk_drawing_area_new();
  gtk_widget_set_has_window(dot, FALSE);
  gtk_widget_set_name(dot, "recording-orb");
  gtk_widget_set_size_request(dot, 44, 44);
  gtk_widget_set_valign(dot, GTK_ALIGN_CENTER);
  g_signal_connect_after(card, "draw", G_CALLBACK(draw_dot), dot);
  gtk_box_pack_start(GTK_BOX(card), dot, FALSE, FALSE, 0);
  GtkWidget *meta = gtk_box_new(GTK_ORIENTATION_VERTICAL, 3);
  gtk_widget_set_size_request(meta, 76, -1);
  gtk_widget_set_valign(meta, GTK_ALIGN_CENTER);
  GtkWidget *title = label("正在录音", "recording-title");
  gtk_widget_set_halign(title, GTK_ALIGN_START);
  gtk_box_pack_start(GTK_BOX(meta), title, FALSE, FALSE, 0);
  meter_label = label("等待声音…", "recording-status");
  gtk_widget_set_halign(meter_label, GTK_ALIGN_START);
  gtk_box_pack_start(GTK_BOX(meta), meter_label, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(card), meta, FALSE, FALSE, 0);
  drawing = gtk_drawing_area_new();
  gtk_widget_set_name(drawing, "recording-wave");
  gtk_widget_set_size_request(drawing, 170, 30);
  gtk_widget_set_valign(drawing, GTK_ALIGN_CENTER);
  g_signal_connect(drawing, "draw", G_CALLBACK(draw_wave), NULL);
  gtk_box_pack_start(GTK_BOX(card), drawing, FALSE, FALSE, 0);
  GtkWidget *side = gtk_box_new(GTK_ORIENTATION_VERTICAL, 5);
  gtk_widget_set_valign(side, GTK_ALIGN_CENTER);
  gtk_widget_set_margin_end(side, 12);
  clock_label = label("00:00", "recording-time");
  gtk_widget_set_halign(clock_label, GTK_ALIGN_END);
  gtk_box_pack_start(GTK_BOX(side), clock_label, FALSE, FALSE, 0);
  GtkWidget *keys = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 4);
  gtk_box_pack_start(GTK_BOX(keys), label("Ctrl", "recording-key"), FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(keys), label("/", "recording-help"), FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(keys), label("空格", "recording-key"), FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(keys), label("结束", "recording-help"), FALSE, FALSE, 0);
  gtk_widget_set_halign(keys, GTK_ALIGN_END);
  gtk_box_pack_start(GTK_BOX(side), keys, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(card), side, FALSE, FALSE, 0);
  return card;
}
void native_overlay(int enabled, double elapsed, int x, int y) {
  if (!enabled) {
    if (timer) {
      g_source_remove(timer);
      timer = 0;
    }
    if (window)
      gtk_widget_destroy(window);
    window = NULL;
    return;
  }
  if (window)
    return;
  if (!native_gui_init())
    return;
  GdkDisplay *display = gdk_display_get_default();
  if (!display)
    return;
  start_time = g_get_monotonic_time() / 1e6 - elapsed;
  last_level = g_get_monotonic_time() / 1e6;
  received = clip = dragging = 0;
  dbfs = -96;
  memset(history, 0, sizeof(history));
  x_position = x;
  y_position = y;
  native_theme_ensure();
  window = gtk_window_new(GTK_WINDOW_POPUP);
  gtk_window_set_title(GTK_WINDOW(window), NAME_INDICATOR);
  gtk_widget_set_name(window, "sixuan-recording-overlay");
  gtk_window_set_decorated(GTK_WINDOW(window), FALSE);
  gtk_window_set_resizable(GTK_WINDOW(window), FALSE);
  gtk_window_set_accept_focus(GTK_WINDOW(window), FALSE);
  gtk_window_set_focus_on_map(GTK_WINDOW(window), FALSE);
  gtk_window_set_skip_taskbar_hint(GTK_WINDOW(window), TRUE);
  gtk_window_set_skip_pager_hint(GTK_WINDOW(window), TRUE);
  gtk_window_set_keep_above(GTK_WINDOW(window), TRUE);
  gtk_window_set_type_hint(GTK_WINDOW(window),
                           GDK_WINDOW_TYPE_HINT_NOTIFICATION);
  gtk_widget_set_app_paintable(window, TRUE);
  GdkVisual *visual = gdk_screen_get_rgba_visual(gtk_widget_get_screen(window));
  if (visual)
    gtk_widget_set_visual(window, visual);
  card = recording_content_new();
  gtk_container_add(GTK_CONTAINER(window), card);
  gtk_widget_add_events(window, GDK_BUTTON_PRESS_MASK |
                                    GDK_BUTTON_RELEASE_MASK |
                                    GDK_POINTER_MOTION_MASK);
  g_signal_connect(window, "button-press-event", G_CALLBACK(press), NULL);
  g_signal_connect(window, "motion-notify-event", G_CALLBACK(motion), NULL);
  g_signal_connect(window, "button-release-event", G_CALLBACK(release), NULL);
  g_signal_connect(window, "size-allocate", G_CALLBACK(position), NULL);
  g_signal_connect(window, "realize", G_CALLBACK(realized), NULL);
  gtk_widget_show_all(window);
  timer = g_timeout_add(80, tick, NULL);
  tick(NULL);
}
void native_level(double rms, double peak) {
  if (!window || !isfinite(rms) || !isfinite(peak))
    return;
  last_level = g_get_monotonic_time() / 1e6;
  received = 1;
  dbfs = MAX(-96, 20 * log10(MAX(rms, 1e-8)));
  double level = rms > .001 ? CLAMP((dbfs + 60) / 60, 0, 1) : 0;
  memmove(history, history + 1, 31 * sizeof(double));
  history[31] = level;
  clip = peak >= .98;
  gtk_widget_queue_draw(drawing);
}

/* Preview uses the exact production card and painters without a popup or timer. */
static gboolean preview_wave(GtkWidget *widget,cairo_t *cr,gpointer unused) {
  double saved[32];memcpy(saved,history,sizeof(history));int saved_clip=clip;clip=0;
  for (int i=0;i<32;i++) history[i]=.15+.75*(.5+.5*sin(i*.65));
  paint_wave(cr,gtk_widget_get_allocated_width(widget),gtk_widget_get_allocated_height(widget),0);
  clip=saved_clip;memcpy(history,saved,sizeof(history));return FALSE;
}
static gboolean preview_capsule_draw(GtkWidget *widget,cairo_t *cr,gpointer data) {
  /* Draw the live production widget tree at miniature scale. No captured image
   * or independent capsule renderer is involved. */
  GtkWidget *content=gtk_bin_get_child(GTK_BIN(data));
  cairo_save(cr);cairo_scale(cr,.84,.84);gtk_widget_draw(content,cr);cairo_restore(cr);return FALSE;
}
GtkWidget *native_overlay_preview(void) {
  GtkWidget *old_card=card,*old_dot=dot,*old_drawing=drawing,*old_clock=clock_label,*old_meter=meter_label;
  int old_state=visual_state;
  GtkWidget *result=recording_content_new();
  g_object_set_data(G_OBJECT(result),"preview",GINT_TO_POINTER(1));
  gtk_widget_set_margin_start(result,0);gtk_widget_set_margin_end(result,0);
  gtk_widget_set_margin_top(result,4);gtk_widget_set_margin_bottom(result,4);
  gtk_style_context_remove_class(gtk_widget_get_style_context(result),"idle");
  gtk_label_set_text(GTK_LABEL(meter_label),"正在收音");gtk_label_set_text(GTK_LABEL(clock_label),"00:04");
  g_signal_handlers_disconnect_by_func(drawing,G_CALLBACK(draw_wave),NULL);
  g_signal_connect(drawing,"draw",G_CALLBACK(preview_wave),NULL);
  card=old_card;dot=old_dot;drawing=old_drawing;clock_label=old_clock;meter_label=old_meter;visual_state=old_state;
  GtkWidget *host=gtk_offscreen_window_new();
  gtk_widget_set_name(host,"sixuan-recording-overlay");
  gtk_container_add(GTK_CONTAINER(host),result);gtk_widget_show_all(host);
  GtkWidget *mini=gtk_drawing_area_new();gtk_widget_set_size_request(mini,418,64);
  g_signal_connect(mini,"draw",G_CALLBACK(preview_capsule_draw),host);
  g_signal_connect_swapped(mini,"destroy",G_CALLBACK(gtk_widget_destroy),host);
  return mini;
}
