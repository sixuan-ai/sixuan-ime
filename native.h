#ifndef SIXUAN_NATIVE_H
#define SIXUAN_NATIVE_H
#include <stdint.h>
#include <stdlib.h>
void *audio_open(const char *);
int audio_read(void *, void *);
void audio_close(void *);
int native_init(int);
int native_public_key(unsigned int, unsigned int, unsigned int);
int native_gui_init(void);
void native_notify(const char *, const char *);
int audio_play(const void *, size_t, const char *);
void native_run(void);
void native_post(void);
void native_quit(void);
char *native_engine(void);
char *native_context(void);
int native_restore(const char *);
int native_begin(uint64_t);
void native_text(const char *, int);
void native_cancel(void);
void native_space(int);
int native_keyboards(void);
int native_voice_commits(void);
int native_voice_preedits(void);
void native_overlay(int, double, int, int);
void native_level(double, double);
void native_clipboard(const char *);
char *native_control(const char *);
uintptr_t native_pinyin_type(void);
void native_pinyin_add(uint64_t, uint64_t, const char *, const char *, int);
void native_pinyin_refresh(uint64_t, uint64_t);
char *native_rime_query(const char *);
void native_enable_pinyin(int);
void native_pinyin_preferences_changed(void);
void native_theme_set(const char *, const char *, const char *);
void native_settings_show(void);
void native_settings_refresh(void);

extern int goPublicBuild(void);
extern char *goBuildName(int);
static inline const char *build_name(int index) {
 static const char *names[26];
 if (!names[index]) names[index] = goBuildName(index);
 return names[index];
}
#define NAME_ENGINE build_name(0)
#define NAME_PINYIN build_name(1)
#define NAME_STATE build_name(2)
#define NAME_BUS build_name(3)
#define NAME_OBJECT build_name(4)
#define NAME_COMPONENT build_name(5)
#define NAME_CLI build_name(6)
#define NAME_PINYIN_TITLE build_name(7)
#define NAME_PINYIN_DESCRIPTION build_name(8)
#define NAME_SETTINGS build_name(9)
#define NAME_PREFERENCES build_name(10)
#define NAME_TITLE build_name(11)
#define NAME_SUBTITLE build_name(12)
#define NAME_AUDIO build_name(13)
#define NAME_MIC build_name(14)
#define NAME_FEEDBACK build_name(15)
#define NAME_PLAYBACK build_name(16)
#define NAME_INDICATOR build_name(17)
#define NAME_SPACE build_name(18)
#define NAME_STOP build_name(19)
#define NAME_LICENSE build_name(20)
#define NAME_RIME_DISTRIBUTION build_name(21)
#define NAME_RIME_CODE build_name(22)
#define NAME_RIME_VERSION build_name(23)
#define NAME_RIME_APP build_name(24)
#define NAME_VERSION build_name(25)
int native_dictionary_build(const char *, const char *, const char *, const char *, const char *);
char *native_pinyin_schema_status(void);
char *native_pinyin_trace(int);

#endif
