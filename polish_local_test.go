package main

import "testing"

func TestLocalPolish(t *testing.T) {
	for _, tt := range []struct{ mode, in, want string }{
		{"basic", "  嗯 今天，呃，明天  ", "今天，明天"},
		{"basic", "嗯嗯声、额头、名额、支吾、唔好、嗯好", "嗯嗯声、额头、名额、支吾、唔好、嗯好"},
		{"basic", "啊，今天好啊。", "今天好啊。"},
		{"basic", "那个设备就是好", "那个设备就是好"},
		{"basic", "那个那个那个，就是就是这样！！", "那个，就是这样！"},
		{"basic", "哎呀啊哈真好", "哎呀啊哈真好"},
		{"basic", "啊哈，啊Q和啊呀", "啊哈，啊Q和啊呀"},
		{"basic", "你好？？？。。。", "你好？。"},
		{"aggressive", "那个今天，就是明天，然后结束。", "今天，明天，结束。"},
		{"aggressive", "我说那个设备，明天然后出发", "我说那个设备，明天然后出发"},
		{"aggressive", "，正常，  内容", "正常，  内容"},
		{"aggressive", "然后，那个，就是，出发", "出发"},
		{"off", " 嗯，那个那个 ", " 嗯，那个那个 "},
	} {
		t.Run(tt.mode+tt.in, func(t *testing.T) {
			if got := localPolish(tt.in, tt.mode); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestLocalPolishFillerBoundaries(t *testing.T) {
	for _, tt := range []struct{ in, basic, aggressive string }{
		{"嗯，今天天气不错啊。", "今天天气不错啊。", "今天天气不错啊。"},
		{"好啊，我们走吧", "好啊，我们走吧", "好啊，我们走吧"},
		{"是啊", "是啊", "是啊"},
		{"啊，那个那个我想说", "那个我想说", "我想说"},
		{"那个，就是我觉得可以", "那个，就是我觉得可以", "我觉得可以"},
		{"额度不够", "额度不够", "额度不够"},
		{"今天，嗯，我们开会", "今天，我们开会", "今天，我们开会"},
		{"嗯", "", ""},
		{"呃、 ,， \t今天，额、明天", "今天，明天", "今天，明天"},
		{"，、, \t正文", "正文", "正文"},
		{"今天，啊，出发吧", "今天，出发吧", "今天，出发吧"},
		{"好的啊。", "好的啊。", "好的啊。"},
	} {
		for _, mode := range []string{"basic", "aggressive"} {
			t.Run(mode+"/"+tt.in, func(t *testing.T) {
				want := tt.basic
				if mode == "aggressive" {
					want = tt.aggressive
				}
				if got := localPolish(tt.in, mode); got != want {
					t.Errorf("got %q want %q", got, want)
				}
			})
		}
	}
}
