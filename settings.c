#include "native.h"
#include <gtk/gtk.h>
#include "theme.h"
#include <math.h>
extern int goPreferenceValue(char *);
extern char *goSetPreference(char *, int);
static GtkWidget *window, *polish[3], *cloud, *local_dict, *pinyin, *sound, *feedback;
static int updating;
static GtkWidget *appearance_page_new(void);
static void appearance_refresh(void);
static GtkWidget *pages, *setting_tabs[3];

static void save_preference(const char *key, int value) {
  char *error = goSetPreference((char *)key, value);
  if (feedback)
    gtk_label_set_text(GTK_LABEL(feedback),
                       *error ? error : "已保存，下次输入或录音时生效");
  free(error);
  native_settings_refresh();
}
static void switch_changed(GObject *widget, GParamSpec *spec, gpointer data) {
  if (!updating)
    save_preference(data, gtk_switch_get_active(GTK_SWITCH(widget)));
}
static void polish_changed(GtkToggleButton *widget, gpointer data) {
  if (!updating && gtk_toggle_button_get_active(widget))
    save_preference("text_polish", GPOINTER_TO_INT(data));
}
static gboolean settings_focus_ring(GtkWidget *widget, cairo_t *cr, gpointer unused) {
  if (gtk_widget_has_visible_focus(widget))
    gtk_render_focus(gtk_widget_get_style_context(widget), cr, 0, 0,
                     gtk_widget_get_allocated_width(widget),
                     gtk_widget_get_allocated_height(widget));
  return FALSE;
}
static void style(GtkWidget *widget, const char *name) {
  gtk_style_context_add_class(gtk_widget_get_style_context(widget), name);
}
static GtkWidget *settings_label(const char *text, const char *name) {
  GtkWidget *label = gtk_label_new(text);
  style(label, name);
  gtk_label_set_xalign(GTK_LABEL(label), 0);
  /* Match the reference's browser line boxes rather than theme-dependent
   * Pango defaults (CJK fallback fonts otherwise add several px per row). */
  double height = !strcmp(name, "settings-title") ? 26 :
                  !strcmp(name, "row-title") ? 20 :
                  !strcmp(name, "row-description") || !strcmp(name, "section") ? 16 :
                  !strcmp(name, "hint") ? 18.4 : 17;
  PangoAttrList *attributes = pango_attr_list_new();
  pango_attr_list_insert(attributes, pango_attr_line_height_new_absolute(height * PANGO_SCALE));
  gtk_label_set_attributes(GTK_LABEL(label), attributes);
  pango_attr_list_unref(attributes);
  return label;
}
static GtkWidget *toggle_row(GtkWidget *card, const char *title,
                             const char *description, const char *key, int last) {
  GtkWidget *row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 16);
  style(row, "setting-row");
  if (last) style(row, "last");
  GtkWidget *text = gtk_box_new(GTK_ORIENTATION_VERTICAL, 3);
  gtk_box_pack_start(GTK_BOX(text), settings_label(title, "row-title"), FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(text), settings_label(description, "row-description"), FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(row), text, TRUE, TRUE, 0);
  GtkWidget *control = gtk_switch_new();
  gtk_widget_set_valign(control, GTK_ALIGN_CENTER);
  gtk_widget_set_name(control, key);
  g_signal_connect_after(control, "draw", G_CALLBACK(settings_focus_ring), NULL);
  gtk_widget_set_tooltip_text(control, title);
  atk_object_set_name(gtk_widget_get_accessible(control), title);
  gtk_box_pack_end(GTK_BOX(row), control, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(card), row, FALSE, FALSE, 0);
  g_signal_connect(control, "notify::active", G_CALLBACK(switch_changed), (gpointer)key);
  return control;
}
void native_settings_refresh(void) {
  if (!window) return;
  updating = 1;
  int value = CLAMP(goPreferenceValue("text_polish"), 0, 2);
  gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(polish[value]), TRUE);
  if (local_dict) gtk_switch_set_active(GTK_SWITCH(local_dict), goPreferenceValue("local_dictionary"));
  if (cloud) gtk_switch_set_active(GTK_SWITCH(cloud), goPreferenceValue("cloud_candidates"));
  gtk_switch_set_active(GTK_SWITCH(pinyin), goPreferenceValue("pinyin_enabled"));
  gtk_switch_set_active(GTK_SWITCH(sound), goPreferenceValue("sound_feedback"));
  appearance_refresh();
  updating = 0;
}
static void tab_changed(GtkToggleButton *button,gpointer data) {
  if (gtk_toggle_button_get_active(button)) gtk_stack_set_visible_child_name(GTK_STACK(pages),data);
}
void native_settings_show(void) {
  if (!native_gui_init()) {
    native_notify("无法打开输入法设置", "请在桌面会话中打开设置。");
    return;
  }
  if (window) {
    native_settings_refresh();
    gtk_window_present(GTK_WINDOW(window));
    return;
  }
  native_theme_ensure();
  updating = 1;
  window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  style(window, "sixuan-settings");
  g_object_add_weak_pointer(G_OBJECT(window), (gpointer *)&window);
  gtk_window_set_title(GTK_WINDOW(window), NAME_SETTINGS);
  gtk_window_set_default_size(GTK_WINDOW(window), 520, -1);
  gtk_window_set_resizable(GTK_WINDOW(window), FALSE);
  gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
  gtk_window_set_icon_name(GTK_WINDOW(window), "input-keyboard");
  GtkWidget *header = gtk_header_bar_new();
  gtk_header_bar_set_title(GTK_HEADER_BAR(header), NAME_SETTINGS);
  gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(header), TRUE);
  gtk_header_bar_set_decoration_layout(GTK_HEADER_BAR(header), ":close");
  gtk_header_bar_set_has_subtitle(GTK_HEADER_BAR(header), FALSE);
  gtk_window_set_titlebar(GTK_WINDOW(window), header);
  GtkWidget *box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
  style(box, "settings-body");
  gtk_container_add(GTK_CONTAINER(window), box);
  GtkWidget *tabs=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,0);
  style(tabs,"segments");gtk_box_set_homogeneous(GTK_BOX(tabs),TRUE);
  gtk_widget_set_margin_bottom(tabs,18);
  gtk_box_pack_start(GTK_BOX(box),tabs,FALSE,FALSE,0);
  pages=gtk_stack_new();
  gtk_box_pack_start(GTK_BOX(box),pages,TRUE,TRUE,0);
  GtkWidget *input_page=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);
  GtkWidget *voice_page=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);
  gtk_stack_add_named(GTK_STACK(pages),input_page,"input");
  gtk_stack_add_named(GTK_STACK(pages),voice_page,"voice");
  gtk_stack_add_named(GTK_STACK(pages),appearance_page_new(),"appearance");
  const char *tab_labels[]={"输入","语音","外观"},*tab_ids[]={"input","voice","appearance"};
  GSList *tab_group=NULL;
  for (int i=0;i<3;i++) {
    GtkWidget *tab=gtk_radio_button_new_with_label(tab_group,tab_labels[i]);setting_tabs[i]=tab;
    tab_group=gtk_radio_button_get_group(GTK_RADIO_BUTTON(tab));
    gtk_toggle_button_set_mode(GTK_TOGGLE_BUTTON(tab),FALSE);style(tab,"segment");
    atk_object_set_name(gtk_widget_get_accessible(tab),tab_labels[i]);
    gtk_box_pack_start(GTK_BOX(tabs),tab,TRUE,TRUE,0);
    g_signal_connect(tab,"toggled",G_CALLBACK(tab_changed),(gpointer)tab_ids[i]);
  }
  gtk_stack_set_visible_child_name(GTK_STACK(pages),"input");
  GtkWidget *input_card = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
  style(input_card, "setting-card");
  gtk_box_pack_start(GTK_BOX(input_page), input_card, FALSE, FALSE, 0);
  pinyin = toggle_row(input_card, "拼音输入", "本机 Rime 拼音", "pinyin_enabled", 0);
  local_dict = toggle_row(input_card, "扩展词库", "使用内置或自行导入的词库；关闭恢复 Rime 原词库，下次输入生效", "local_dictionary", 0);
  cloud = toggle_row(input_card, "云端候选", goPublicBuild() ? "免 API Key，会将拼音发送到思玄服务器及候选后端" : "会向云端发送当前输入的拼音", "cloud_candidates", 0);
  GtkWidget *voice_card=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);
  style(voice_card,"setting-card");gtk_box_pack_start(GTK_BOX(voice_page),voice_card,FALSE,FALSE,0);
  sound = toggle_row(voice_card, "录音提示音", "开始、结束和取消时播放", "sound_feedback", 1);
  gtk_box_pack_start(GTK_BOX(voice_page), settings_label("语音文字整理", "section"), FALSE, FALSE, 0);
  GtkWidget *polish_card = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
  style(polish_card, "setting-card");
  style(polish_card, "polish-card");
  gtk_box_pack_start(GTK_BOX(voice_page), polish_card, FALSE, FALSE, 0);
  GtkWidget *segments = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 0);
  style(segments, "linked");
  style(segments, "segments");
  gtk_widget_set_name(segments, "text_polish");
  gtk_box_pack_start(GTK_BOX(polish_card), segments, FALSE, FALSE, 0);
  const char *choices[] = {"关闭", "基础", "增强"};
  GSList *group = NULL;
  for (int i = 0; i < 3; i++) {
    polish[i] = gtk_radio_button_new_with_label(group, choices[i]);
    group = gtk_radio_button_get_group(GTK_RADIO_BUTTON(polish[i]));
    gtk_toggle_button_set_mode(GTK_TOGGLE_BUTTON(polish[i]), FALSE);
    style(polish[i], "segment");
    PangoAttrList *line = pango_attr_list_new();
    pango_attr_list_insert(line, pango_attr_line_height_new_absolute(18 * PANGO_SCALE));
    gtk_label_set_attributes(GTK_LABEL(gtk_bin_get_child(GTK_BIN(polish[i]))), line);
    pango_attr_list_unref(line);
    g_signal_connect_after(polish[i], "draw", G_CALLBACK(settings_focus_ring), NULL);
    gtk_widget_set_hexpand(polish[i], TRUE);
    gtk_box_pack_start(GTK_BOX(segments), polish[i], TRUE, TRUE, 0);
    g_signal_connect(polish[i], "toggled", G_CALLBACK(polish_changed), GINT_TO_POINTER(i));
  }
  gtk_box_set_homogeneous(GTK_BOX(segments), TRUE);
  GtkWidget *hint = settings_label("整理只影响语音输入的最终文字。增强整理可能稍慢，超时会保留已识别的文字。", "hint");
  gtk_label_set_line_wrap(GTK_LABEL(hint), TRUE);
  gtk_label_set_max_width_chars(GTK_LABEL(hint), 42);
  gtk_box_pack_start(GTK_BOX(polish_card), hint, FALSE, FALSE, 0);
  GtkWidget *foot = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 7);
  gtk_widget_set_margin_top(foot, 6);
  gtk_box_pack_start(GTK_BOX(box), foot, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(foot), settings_label("●", "saved-dot"), FALSE, FALSE, 0);
  feedback = settings_label("设置自动保存", "secondary");
  gtk_label_set_line_wrap(GTK_LABEL(feedback), TRUE);
  gtk_label_set_max_width_chars(GTK_LABEL(feedback), 24);
  gtk_widget_set_valign(feedback, GTK_ALIGN_CENTER);
  gtk_box_pack_start(GTK_BOX(foot), feedback, TRUE, TRUE, 0);
  GtkWidget *close = gtk_button_new_with_label("完成");
  style(close, "primary");
  PangoAttrList *line = pango_attr_list_new();
  pango_attr_list_insert(line, pango_attr_line_height_new_absolute(20 * PANGO_SCALE));
  gtk_label_set_attributes(GTK_LABEL(gtk_bin_get_child(GTK_BIN(close))), line);
  pango_attr_list_unref(line);
  g_signal_connect_after(close, "draw", G_CALLBACK(settings_focus_ring), NULL);
  gtk_widget_set_valign(close, GTK_ALIGN_CENTER);
  gtk_box_pack_end(GTK_BOX(foot), close, FALSE, FALSE, 0);
  g_signal_connect_swapped(close, "clicked", G_CALLBACK(gtk_widget_destroy), window);
  native_settings_refresh();
  gtk_widget_show_all(window);
  appearance_refresh();
  gtk_window_present(GTK_WINDOW(window));
}

extern char *goSetAppearance(char *,char *);
static GtkWidget *swatches[11],*color_buttons[2],*color_labels[2],*contrast_label,*base_hint,*opacity_scale,*opacity_label;
static GtkWidget *radius_buttons[3],*glow_buttons[3],*size_buttons[3],*live_preview;
static const char *radius_ids[]={"capsule","rounded","square"};
static const char *glow_ids[]={"off","soft","strong"};
static const char *size_ids[]={"small","standard","large"};
static void save_appearance(const char *key,const char *value) {
  if (updating) return;
  char *error=goSetAppearance((char *)key,(char *)value);
  if (feedback) gtk_label_set_text(GTK_LABEL(feedback),*error?error:"外观已保存并应用");
  free(error);
  native_settings_refresh();
}
static void appearance_selected(GtkToggleButton *button,gpointer data) {
  if (updating || !gtk_toggle_button_get_active(button)) return;
  const char *key=g_object_get_data(G_OBJECT(button),"appearance-key");
  save_appearance(key,data);
}
static void preset_selected(GtkToggleButton *button,gpointer data) {
  if (updating || !gtk_toggle_button_get_active(button)) return;
  int index=GPOINTER_TO_INT(data);if (index==10) return;
  char key[40];g_snprintf(key,sizeof(key),"preset-%d-id",index);
  char *id=theme_string(key);if (id) {save_appearance("preset",id);g_free(id);}
}
static gboolean swatch_draw(GtkWidget *widget,cairo_t *cr,gpointer data) {
  int index=GPOINTER_TO_INT(data);char key[40];
  GdkRGBA accent,base;
  if (index<10) {g_snprintf(key,sizeof(key),"preset-%d-accent",index);accent=theme_color(key);g_snprintf(key,sizeof(key),"preset-%d-base",index);base=theme_color(key);}
  else {accent=theme_color("accent");base=theme_color("base");}
  double x=gtk_widget_get_allocated_width(widget)/2.,y=20;
  gdk_cairo_set_source_rgba(cr,&base);cairo_arc(cr,x,y,18,0,2*G_PI);cairo_fill(cr);
  gdk_cairo_set_source_rgba(cr,&accent);cairo_arc(cr,x,y,index==10?18:9,index==10?-G_PI/2:0,index==10?G_PI/2:2*G_PI);if(index==10)cairo_close_path(cr);cairo_fill(cr);
  return FALSE;
}
static gboolean color_draw(GtkWidget *widget,cairo_t *cr,gpointer data) {
  GdkRGBA color=theme_color(data);gdk_cairo_set_source_rgba(cr,&color);
  cairo_arc(cr,12,12,11,0,2*G_PI);cairo_fill(cr);return FALSE;
}
static void choose_color(GtkButton *button,gpointer data) {
  const char *key=data;char *current=theme_string(key);
  GtkWidget *dialog=gtk_color_chooser_dialog_new(!strcmp(key,"accent")?"主色":"底色",GTK_WINDOW(window));
  gtk_color_chooser_set_use_alpha(GTK_COLOR_CHOOSER(dialog),FALSE);
  GdkRGBA colors[10],color;gdk_rgba_parse(&color,current?current:"#000000");g_free(current);
  for(int i=0;i<10;i++){char k[40];g_snprintf(k,sizeof(k),"preset-%d-accent",i);colors[i]=theme_color(k);}
  gtk_color_chooser_add_palette(GTK_COLOR_CHOOSER(dialog),GTK_ORIENTATION_HORIZONTAL,10,10,colors);
  gtk_color_chooser_set_rgba(GTK_COLOR_CHOOSER(dialog),&color);
  if (gtk_dialog_run(GTK_DIALOG(dialog))==GTK_RESPONSE_OK) {
    gtk_color_chooser_get_rgba(GTK_COLOR_CHOOSER(dialog),&color);
    char hex[8];g_snprintf(hex,sizeof(hex),"#%02X%02X%02X",(int)round(color.red*255),(int)round(color.green*255),(int)round(color.blue*255));
    save_appearance(key,hex);
  }
  gtk_widget_destroy(dialog);
}
static GtkWidget *appearance_card(GtkWidget *page,const char *title) {
  gtk_box_pack_start(GTK_BOX(page),settings_label(title,"section"),FALSE,FALSE,0);
  GtkWidget *card=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);style(card,"setting-card");
  gtk_box_pack_start(GTK_BOX(page),card,FALSE,FALSE,0);return card;
}
static GtkWidget *appearance_row(GtkWidget *card,const char *title,const char *description) {
  GtkWidget *row=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,12),*labels=gtk_box_new(GTK_ORIENTATION_VERTICAL,3);style(row,"setting-row");
  gtk_box_pack_start(GTK_BOX(labels),settings_label(title,"row-title"),FALSE,FALSE,0);
  if (description) gtk_box_pack_start(GTK_BOX(labels),settings_label(description,"row-description"),FALSE,FALSE,0);
  gtk_box_pack_start(GTK_BOX(row),labels,TRUE,TRUE,0);gtk_box_pack_start(GTK_BOX(card),row,FALSE,FALSE,0);return row;
}
static void style_segment(GtkWidget *card,const char *title,const char *key,const char **ids,const char **labels,GtkWidget **buttons) {
  GtkWidget *row=appearance_row(card,title,NULL),*seg=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,0);style(seg,"segments");
  gtk_box_pack_end(GTK_BOX(row),seg,FALSE,FALSE,0);GSList *group=NULL;
  for (int i=0;i<3;i++) {buttons[i]=gtk_radio_button_new_with_label(group,labels[i]);group=gtk_radio_button_get_group(GTK_RADIO_BUTTON(buttons[i]));
    gtk_toggle_button_set_mode(GTK_TOGGLE_BUTTON(buttons[i]),FALSE);style(buttons[i],"segment");
    char *name=g_strdup_printf("%s：%s",title,labels[i]);atk_object_set_name(gtk_widget_get_accessible(buttons[i]),name);g_free(name);
    g_object_set_data(G_OBJECT(buttons[i]),"appearance-key",(gpointer)key);
    g_signal_connect(buttons[i],"toggled",G_CALLBACK(appearance_selected),(gpointer)ids[i]);gtk_box_pack_start(GTK_BOX(seg),buttons[i],TRUE,TRUE,0);
  }
}
static void opacity_changed(GtkRange *range,gpointer data) {
  char value[32];g_ascii_dtostr(value,sizeof(value),gtk_range_get_value(range)/100.);save_appearance("opacity",value);
}
static void restore_appearance(GtkButton *button,gpointer data) {save_appearance("reset","");}
GtkWidget *native_candidate_preview(void) {
  GtkWidget *outer=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);style(outer,"candidate-preview");style(outer,"candidate-popup-boxpointer");
  GtkWidget *content=gtk_box_new(GTK_ORIENTATION_VERTICAL,8);style(content,"candidate-popup-content");gtk_box_pack_start(GTK_BOX(outer),content,FALSE,FALSE,0);
  GtkWidget *preedit=gtk_label_new("ni hao");style(preedit,"candidate-popup-text");gtk_label_set_xalign(GTK_LABEL(preedit),0);gtk_box_pack_start(GTK_BOX(content),preedit,FALSE,FALSE,0);
  GtkWidget *row=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,0);gtk_box_pack_start(GTK_BOX(content),row,FALSE,FALSE,0);
  const char *items[]={"你好","拟好","你号"};
  for(int i=0;i<3;i++) {
    GtkWidget *item=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,6);style(item,"candidate-box");if(!i)style(item,"selected");
    char number[8];g_snprintf(number,sizeof(number),"%d",i+1);GtkWidget *index=gtk_label_new(number);style(index,"candidate-index");gtk_box_pack_start(GTK_BOX(item),index,FALSE,FALSE,0);
    gtk_box_pack_start(GTK_BOX(item),gtk_label_new(items[i]),FALSE,FALSE,0);
    if(i==2){GtkWidget *tag=gtk_label_new("〔云〕");style(tag,"source-tag");gtk_box_pack_start(GTK_BOX(item),tag,FALSE,FALSE,0);}
    gtk_box_pack_start(GTK_BOX(row),item,FALSE,FALSE,0);
  }
  GtkWidget *nav=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,4);style(nav,"candidate-page-button-box");gtk_box_pack_start(GTK_BOX(row),nav,FALSE,FALSE,0);
  for(int i=0;i<2;i++){GtkWidget *button=gtk_label_new(i?"›":"‹");style(button,"candidate-page-button");gtk_box_pack_start(GTK_BOX(nav),button,FALSE,FALSE,0);}
  return outer;
}
static GtkWidget *appearance_page_new(void) {
  GtkWidget *page=gtk_box_new(GTK_ORIENTATION_VERTICAL,0);GtkWidget *preview=appearance_card(page,"实时预览");
  live_preview=gtk_box_new(GTK_ORIENTATION_VERTICAL,8);
  gtk_widget_set_name(live_preview,"sixuan-recording-overlay");
  gtk_box_pack_start(GTK_BOX(preview),live_preview,FALSE,FALSE,10);
  gtk_box_pack_start(GTK_BOX(live_preview),native_candidate_preview(),FALSE,FALSE,0);
  gtk_box_pack_start(GTK_BOX(live_preview),native_overlay_preview(),FALSE,FALSE,0);
  GtkWidget *skin=appearance_card(page,"皮肤"),*grid=gtk_grid_new();gtk_grid_set_column_homogeneous(GTK_GRID(grid),TRUE);gtk_grid_set_row_spacing(GTK_GRID(grid),8);gtk_grid_set_column_spacing(GTK_GRID(grid),8);
  gtk_box_pack_start(GTK_BOX(skin),grid,FALSE,FALSE,10);GSList *group=NULL;
  for(int i=0;i<11;i++) {
    char key[40];g_snprintf(key,sizeof(key),"preset-%d-name",i);char *name=i==10?g_strdup("自定义"):theme_string(key);
    swatches[i]=gtk_radio_button_new(group);group=gtk_radio_button_get_group(GTK_RADIO_BUTTON(swatches[i]));gtk_toggle_button_set_mode(GTK_TOGGLE_BUTTON(swatches[i]),FALSE);style(swatches[i],"swatch");
    atk_object_set_name(gtk_widget_get_accessible(swatches[i]),name?name:"皮肤");
    GtkWidget *content=gtk_box_new(GTK_ORIENTATION_VERTICAL,2),*dot=gtk_drawing_area_new();gtk_widget_set_size_request(dot,40,40);
    g_signal_connect(dot,"draw",G_CALLBACK(swatch_draw),GINT_TO_POINTER(i));gtk_box_pack_start(GTK_BOX(content),dot,FALSE,FALSE,0);gtk_box_pack_start(GTK_BOX(content),gtk_label_new(name),FALSE,FALSE,0);g_free(name);
    gtk_widget_show_all(content);gtk_container_add(GTK_CONTAINER(swatches[i]),content);gtk_grid_attach(GTK_GRID(grid),swatches[i],i%5,i/5,1,1);
    g_signal_connect(swatches[i],"toggled",G_CALLBACK(preset_selected),GINT_TO_POINTER(i));
  }
  gtk_widget_set_no_show_all(swatches[10],TRUE);
  for(int i=0;i<2;i++) {
    GtkWidget *row=appearance_row(skin,i?"底色":"主色",i?"浅色底自动切换为深色文字":"选中候选、开关、按钮、波形");
    color_buttons[i]=gtk_button_new();
    GtkWidget *color_box=gtk_box_new(GTK_ORIENTATION_HORIZONTAL,8),*color_dot=gtk_drawing_area_new();
    gtk_widget_set_size_request(color_dot,24,24);g_signal_connect(color_dot,"draw",G_CALLBACK(color_draw),i?"base":"accent");
    color_labels[i]=gtk_label_new("");gtk_box_pack_start(GTK_BOX(color_box),color_dot,FALSE,FALSE,0);gtk_box_pack_start(GTK_BOX(color_box),color_labels[i],FALSE,FALSE,0);gtk_container_add(GTK_CONTAINER(color_buttons[i]),color_box);style(color_buttons[i],"color-button");atk_object_set_name(gtk_widget_get_accessible(color_buttons[i]),i?"底色":"主色");
    gtk_box_pack_end(GTK_BOX(row),color_buttons[i],FALSE,FALSE,0);g_signal_connect(color_buttons[i],"clicked",G_CALLBACK(choose_color),i?"base":"accent");
    if(!i){contrast_label=settings_label("","secondary");gtk_box_pack_start(GTK_BOX(skin),contrast_label,FALSE,FALSE,0);}else {base_hint=settings_label("","secondary");gtk_box_pack_start(GTK_BOX(skin),base_hint,FALSE,FALSE,0);}
  }
  GtkWidget *options=appearance_card(page,"样式");
  const char *radius_labels[]={"胶囊","圆润","方正"},*glow_labels[]={"关闭","柔和","强烈"},*size_labels[]={"小","标准","大"};
  style_segment(options,"圆角","radius",radius_ids,radius_labels,radius_buttons);
  style_segment(options,"光晕","glow",glow_ids,glow_labels,glow_buttons);
  style_segment(options,"候选字号","candidate_size",size_ids,size_labels,size_buttons);
  GtkWidget *row=appearance_row(options,"候选窗不透明度",NULL);opacity_label=settings_label("94%","secondary");gtk_box_pack_start(GTK_BOX(row),opacity_label,FALSE,FALSE,0);
  opacity_scale=gtk_scale_new_with_range(GTK_ORIENTATION_HORIZONTAL,70,100,1);gtk_scale_set_draw_value(GTK_SCALE(opacity_scale),FALSE);gtk_widget_set_size_request(opacity_scale,150,-1);atk_object_set_name(gtk_widget_get_accessible(opacity_scale),"候选窗不透明度");gtk_box_pack_end(GTK_BOX(row),opacity_scale,FALSE,FALSE,0);g_signal_connect(opacity_scale,"value-changed",G_CALLBACK(opacity_changed),NULL);
  GtkWidget *reset=gtk_button_new_with_label("恢复默认");gtk_style_context_add_class(gtk_widget_get_style_context(reset),"ghost");atk_object_set_name(gtk_widget_get_accessible(reset),"恢复默认皮肤");gtk_widget_set_halign(reset,GTK_ALIGN_START);gtk_box_pack_start(GTK_BOX(page),reset,FALSE,FALSE,0);g_signal_connect(reset,"clicked",G_CALLBACK(restore_appearance),NULL);
  return page;
}
static void appearance_refresh(void) {
  if(!window || !opacity_scale) return;
  int old=updating;updating=1;
  char *preset=theme_string("preset");int selected=10;
  for(int i=0;i<10;i++) {char key[40];g_snprintf(key,sizeof(key),"preset-%d-id",i);char *id=theme_string(key);if(!g_strcmp0(id,preset))selected=i;g_free(id);}
  g_free(preset);gtk_widget_set_visible(swatches[10],selected==10);gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(swatches[selected]),TRUE);
  for(int i=0;i<2;i++){char *color=theme_string(i?"base":"accent");gtk_label_set_text(GTK_LABEL(color_labels[i]),color);g_free(color);}
  const char *keys[]={"radius","glow","candidate_size"};const char **ids[]={radius_ids,glow_ids,size_ids};GtkWidget **buttons[]={radius_buttons,glow_buttons,size_buttons};
  for(int k=0;k<3;k++){char *value=theme_string(keys[k]);for(int i=0;i<3;i++)if(!g_strcmp0(value,ids[k][i]))gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(buttons[k][i]),TRUE);g_free(value);}
  double ratio=theme_number("contrast");char text[180];
  if(ratio<3){g_snprintf(text,sizeof(text),"颜色对比度偏低（%.1f:1），选中项可能不清晰",ratio);style(contrast_label,"contrast-warning");}
  else{g_snprintf(text,sizeof(text),"与底色对比度 %.1f:1",ratio);gtk_style_context_remove_class(gtk_widget_get_style_context(contrast_label),"contrast-warning");}
  gtk_label_set_text(GTK_LABEL(contrast_label),text);
  gtk_label_set_text(GTK_LABEL(base_hint),theme_number("light")?"浅色底 · 深色文字":"深色底 · 浅色文字");
  gtk_range_set_value(GTK_RANGE(opacity_scale),theme_number("opacity")*100);g_snprintf(text,sizeof(text),"%.0f%%",theme_number("opacity")*100);gtk_label_set_text(GTK_LABEL(opacity_label),text);
  gtk_widget_queue_draw(live_preview);updating=old;
}
void native_theme_previews_changed(void) {
  if(window) {appearance_refresh();gtk_widget_queue_draw(window);}
}
