package main

/*
#include "native.h"
*/
import "C"
import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"unsafe"
)

type event struct {
	pinyinID          uint64
	choices           []cloudCandidate
	query             string
	kind              string
	generation        uint64
	text              string
	rms, peak         float64
	elapsed           float64
	warm              bool
	requests          int
	endUpload, endAck float64
	polls             int
}

func levels(b []byte) (float64, float64) {
	var sum, peak float64
	n := len(b) / 2
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(b[i*2:]))) / 32768
		sum += v * v
		peak = math.Max(peak, math.Abs(v))
	}
	if n == 0 {
		return 0, 0
	}
	return math.Sqrt(sum / float64(n)), peak
}
func capture(ctx context.Context, stop <-chan struct{}, source string, out chan<- []byte, emit func(event)) {
	defer close(out)
	s := C.CString(source)
	audio := C.audio_open(s)
	C.free(unsafe.Pointer(s))
	if audio == nil {
		emit(event{kind: "error", text: "无法打开麦克风"})
		return
	}
	defer func() { C.audio_close(audio); emit(event{kind: "mic_stopped"}) }()
	first := true
	meter := make([]byte, 0, 1920)
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		default:
		}
		frame := make([]byte, 640)
		if C.audio_read(audio, unsafe.Pointer(&frame[0])) < 0 {
			emit(event{kind: "error", text: "麦克风读取失败"})
			return
		}
		if first {
			emit(event{kind: "ready"})
			first = false
		}
		meter = append(meter, frame...)
		if len(meter) >= 1920 {
			r, p := levels(meter)
			emit(event{kind: "level", rms: r, peak: p})
			meter = meter[:0]
		}
		select {
		case out <- frame:
		case <-ctx.Done():
			return
		}
	}
}

func pcmReader(ctx context.Context, in io.Reader, out chan<- []byte) {
	defer close(out)
	for {
		b := make([]byte, 640)
		n, e := io.ReadFull(in, b)
		if n > 0 {
			select {
			case out <- b:
			case <-ctx.Done():
				return
			}
		}
		if e != nil {
			return
		}
	}
}
