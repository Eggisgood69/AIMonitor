package main

import "testing"

func TestScreenSettings(t *testing.T) {
	def := screenEff{Bri: 160, Rot: 0, Led: 0, ARot: true}
	m := map[string]*ScreenSet{}

	// 沒設過：用共用值
	if got := def.with(m["a"]); got != def {
		t.Fatalf("unset screen = %+v, want %+v", got, def)
	}

	// 每塊各自設定，互不影響
	for _, c := range [][3]string{{"a", "bri", "40"}, {"a", "rot", "2"}, {"b", "led", "50"}} {
		if !applyScreenSetting(m, c[0], c[1], c[2]) {
			t.Fatalf("apply %v failed", c)
		}
	}
	if got, want := def.with(m["a"]), (screenEff{Bri: 40, Rot: 2, Led: 0, ARot: false}); got != want {
		t.Fatalf("screen a = %+v, want %+v", got, want)
	}
	if got, want := def.with(m["b"]), (screenEff{Bri: 160, Rot: 0, Led: 50, ARot: true}); got != want {
		t.Fatalf("screen b = %+v, want %+v", got, want)
	}

	// 選回自動轉向：保留上次的固定方向（關掉自動時用）
	applyScreenSetting(m, "a", "rot", "auto")
	if got := def.with(m["a"]); !got.ARot || got.Rot != 2 {
		t.Fatalf("auto rotation = %+v", got)
	}

	// 不合法的值不改
	for _, c := range [][3]string{{"a", "bri", "3"}, {"a", "led", "101"}, {"a", "rot", "4"}, {"a", "x", "1"}, {"", "bri", "50"}} {
		if applyScreenSetting(m, c[0], c[1], c[2]) {
			t.Fatalf("apply %v should fail", c)
		}
	}
	if got := def.with(m["a"]); got.Bri != 40 || got.Led != 0 {
		t.Fatalf("invalid values changed screen a: %+v", got)
	}
}
