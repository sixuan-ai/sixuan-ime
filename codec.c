#include "native.h"
#include <pulse/error.h>
#include <pulse/simple.h>
#include <string.h>
#include <stdio.h>
void *audio_open(const char *device) {
  pa_sample_spec spec = {PA_SAMPLE_S16LE, 16000, 1};
  pa_buffer_attr attr = {.maxlength = (uint32_t)-1,
                         .tlength = (uint32_t)-1,
                         .prebuf = (uint32_t)-1,
                         .minreq = (uint32_t)-1,
                         .fragsize = 640};
  int error;
  return pa_simple_new(
      NULL, NAME_AUDIO, PA_STREAM_RECORD,
      (!device || !strcmp(device, "@DEFAULT_SOURCE@")) ? NULL : device,
      NAME_MIC, &spec, NULL, &attr, &error);
}
int audio_read(void *s, void *out) {
  int error;
  return pa_simple_read(s, out, 640, &error);
}
void audio_close(void *s) { pa_simple_free(s); }

int audio_play(const void *data, size_t n, const char *kind) {
  pa_sample_spec spec = {PA_SAMPLE_S16LE, 24000, 1};
  int error;
  char name[128];
  snprintf(name, sizeof(name), NAME_FEEDBACK, kind);
  const char *sink = getenv("PULSE_SINK");
  pa_simple *s = pa_simple_new(NULL, NAME_PLAYBACK, PA_STREAM_PLAYBACK,
                               sink && *sink ? sink : NULL, name, &spec, NULL,
                               NULL, &error);
  if (!s)
    return -1;
  int r = pa_simple_write(s, data, n, &error);
  if (r == 0)
    r = pa_simple_drain(s, &error);
  pa_simple_free(s);
  return r;
}
