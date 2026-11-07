package main

import "testing"

func TestLateFinalCannotReopenInsertion(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	previous := app
	defer func() { app = previous }()
	for _, phase := range []string{"idle", "inserting"} {
		app = &application{phase: phase, generation: 23, last: "已处理的结果"}
		events <- event{kind: "final", generation: 23, text: "重复到达的最终结果"}
		goDrain()
		if app.phase != phase || app.last != "已处理的结果" {
			t.Fatal("duplicate completion changed delivery state", phase, app.phase)
		}
	}
}
