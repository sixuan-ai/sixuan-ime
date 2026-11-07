package main

/*
#include "native.h"
*/
import "C"
import (
	"context"
	"sync"
	"time"
)

type cloudJob struct {
	id, revision uint64
	input, query string
	ctx          context.Context
	cancel       context.CancelFunc
	settings     config
}

var cloudJobs = make(chan cloudJob, 1)
var pinyinState struct {
	id, revision uint64
	query        string
	cancel       context.CancelFunc
	timer        *time.Timer
}
var cloudWorkerOnce sync.Once

func startCandidateWorker() {
	cloudWorkerOnce.Do(func() {
		go func() {
			for job := range cloudJobs {
				if result := runCandidateJob(job, queryCloud); result != nil {
					post(*result)
				}
			}
		}()
	})
}

// Cancellation discards an obsolete edit; a deadline is a current lookup failure.
func runCandidateJob(job cloudJob, query func(context.Context, string, config) ([]cloudCandidate, error)) *event {
	defer job.cancel()
	if job.ctx.Err() == context.Canceled {
		return nil
	}
	var rows []cloudCandidate
	err := job.ctx.Err()
	if err == nil {
		rows, err = query(job.ctx, job.input, job.settings)
	}
	if job.ctx.Err() == context.Canceled {
		return nil
	}
	if err == nil && job.ctx.Err() == nil {
		return &event{kind: "pinyin_cloud", pinyinID: job.id, generation: job.revision, choices: rows}
	}
	if err == nil {
		err = job.ctx.Err()
	}
	return &event{kind: "pinyin_error", text: cloudErrorMessage(err), pinyinID: job.id, generation: job.revision}
}

//export goPinyinEnabled
func goPinyinEnabled() C.int {
	if app != nil && app.config.PinyinEnabled {
		return 1
	}
	return 0
}

//export goPinyinChanged
func goPinyinChanged(id, revision C.uint64_t, raw, preedit *C.char) {
	input := C.GoString(raw)
	engineID, rev := uint64(id), uint64(revision)
	if input == "" && pinyinState.id != engineID {
		return
	}
	if pinyinState.cancel != nil {
		pinyinState.cancel()
		pinyinState.cancel = nil
	}
	if pinyinState.timer != nil {
		pinyinState.timer.Stop()
	}
	pinyinState.id = engineID
	pinyinState.revision = rev
	pinyinState.query = normalizePinyin(input)
	if app == nil || !app.config.PinyinEnabled || input == "" {
		return
	}
	for _, word := range phraseCandidates(input) {
		text, freeT := cstr(word.Text)
		source, freeS := cstr(word.Source)
		C.native_pinyin_add(id, revision, text, source, 0)
		freeT()
		freeS()
	}
	if !app.config.CloudCandidates || len(pinyinState.query) < 4 || !validPinyin(pinyinState.query) {
		return
	}
	segmented := C.GoString(preedit)
	if normalizePinyin(segmented) != pinyinState.query {
		segmented = input
	}
	startCandidateWorker()
	query := pinyinState.query
	pinyinState.timer = time.AfterFunc(time.Duration(app.config.CloudDebounceMS)*time.Millisecond, func() {
		post(event{kind: "pinyin_fetch", pinyinID: engineID, generation: rev, text: segmented, query: query})
	})
}

//export goPinyinLearn
func goPinyinLearn(raw, text *C.char) {
	key, value := normalizePinyin(C.GoString(raw)), C.GoString(text)
	if validPhrase(key, value) {
		go learnWord(key, value)
	}
}
func pinyinEvent(e event) {
	if e.kind == "dictionary_status" {
		if app != nil {
			app.dictionaryError = e.text
		}
		return
	}
	if pinyinState.id != e.pinyinID || pinyinState.revision != e.generation {
		return
	}
	switch e.kind {
	case "pinyin_fetch":
		// A debounce timer may already have posted before preferences changed.
		if app == nil || !app.config.PinyinEnabled || !app.config.CloudCandidates || e.query != pinyinState.query {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(app.config.CloudTimeoutMS)*time.Millisecond)
		pinyinState.cancel = cancel
		job := cloudJob{id: e.pinyinID, revision: e.generation, input: e.text, query: e.query, ctx: ctx, cancel: cancel, settings: app.config}
		select {
		case cloudJobs <- job:
		default:
			select {
			case old := <-cloudJobs:
				old.cancel()
			default:
			}
			select {
			case cloudJobs <- job:
			default:
				cancel()
			}
		}
	case "pinyin_cloud":
		if !app.config.PinyinEnabled || !app.config.CloudCandidates {
			return
		}
		for _, row := range e.choices {
			if normalizePinyin(row.Syllable) != pinyinState.query {
				continue
			}
			text, freeT := cstr(row.Text)
			source, freeS := cstr("云")
			C.native_pinyin_add(C.uint64_t(e.pinyinID), C.uint64_t(e.generation), text, source, 1)
			freeT()
			freeS()
		}
		C.native_pinyin_refresh(C.uint64_t(e.pinyinID), C.uint64_t(e.generation))
		app.cloudError = ""
	case "pinyin_error":
		app.cloudError = e.text
	}
}
