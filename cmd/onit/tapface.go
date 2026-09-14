package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// tapFace makes the device face itself the spin control: a click anywhere on
// the circle fires onTap. It has no chrome of its own — the face's small
// "click to spin" caption is the affordance.
type tapFace struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTapFace(content fyne.CanvasObject, onTap func()) *tapFace {
	t := &tapFace{content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapFace) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

func (t *tapFace) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}
