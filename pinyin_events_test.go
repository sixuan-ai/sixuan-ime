package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueuedCloudFetchHonorsCurrentPreferences(t *testing.T) {
	oldApp, oldState, oldJobs := app, pinyinState, cloudJobs
	defer func() { app = oldApp; pinyinState = oldState; cloudJobs = oldJobs }()
	cloudJobs = make(chan cloudJob, 1)
	pinyinState.id = 42
	pinyinState.revision = 7
	pinyinState.query = "nihao"
	for _, c := range []config{{PinyinEnabled: true, CloudCandidates: false}, {PinyinEnabled: false, CloudCandidates: true}} {
		app = &application{config: c}
		pinyinEvent(event{kind: "pinyin_fetch", pinyinID: 42, generation: 7, text: "ni hao", query: "nihao"})
		select {
		case j := <-cloudJobs:
			j.cancel()
			t.Error("queued fetch still scheduled after disabling pinyin/cloud")
		default:
		}
	}
}

func TestCloudWorkerTimeoutReportsFailureButCancelIsSilent(t *testing.T) {
	for _, mode := range []string{"success", "error", "timeout", "expired", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "timeout" || mode == "expired" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			}
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			if mode == "expired" {
				<-ctx.Done()
			}
			called := false
			result := runCandidateJob(cloudJob{id: 42, revision: 9, ctx: ctx, cancel: cancel}, func(ctx context.Context, _ string, _ config) ([]cloudCandidate, error) {
				called = true
				if mode == "timeout" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if mode == "error" {
					return nil, errors.New("private upstream details")
				}
				return []cloudCandidate{{Text: "你好", Syllable: "nihao"}}, nil
			})
			if mode == "cancel" {
				if result != nil || called {
					t.Fatal("obsolete request not discarded")
				}
				return
			}
			if result == nil || result.pinyinID != 42 || result.generation != 9 {
				t.Fatal("missing current response")
			}
			if mode == "success" {
				if result.kind != "pinyin_cloud" || len(result.choices) != 1 {
					t.Fatal("valid response lost")
				}
			} else if result.kind != "pinyin_error" || result.text == "" || result.text == "private upstream details" {
				t.Fatal("failed lookup silent or leaked details")
			}
		})
	}
}
