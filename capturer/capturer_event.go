package main

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

type InputController struct {
	conn *xgb.Conn
	root xproto.Window
}

// NewInputController 连接到指定的 display (:99)
func NewInputController(display string) (*InputController, error) {
	// 连接 X Server
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, err
	}

	// 初始化 XTEST 扩展 (用于模拟输入)
	if err := xtest.Init(c); err != nil {
		return nil, err
	}

	// 获取 Root 窗口 (通常是整个屏幕)
	setup := xproto.Setup(c)
	root := setup.Roots[0].Root

	return &InputController{
		conn: c,
		root: root,
	}, nil
}

func (ic *InputController) Close() {
	if ic.conn != nil {
		ic.conn.Close()
	}
}

// MoveMouse 移动鼠标到绝对坐标 (x, y)
func (ic *InputController) MoveMouse(x, y int16) {
	// 使用 WarpPointer 直接瞬移鼠标指针
	// SrcWindow: None, DstWindow: Root
	// SrcX/Y, SrcWidth/Height: 0 (忽略源)
	// DstX, DstY: 目标坐标
	xproto.WarpPointer(ic.conn, xproto.Window(0), ic.root, 0, 0, 0, 0, x, y)

	// 刷新缓冲区，确保指令立即发出
	// ic.conn.Sync() // 如果觉得卡顿可以把 Sync 去掉，依靠系统自动 flush
}

// MouseClick 模拟鼠标点击
// button: 1=左键, 2=中键, 3=右键, 4=滚轮上, 5=滚轮下
// isPress: true=按下, false=抬起
func (ic *InputController) MouseEvent(button byte, isPress bool) {
	// XTEST 模拟输入
	// type: ButtonPress / ButtonRelease
	// detail: 按钮编号
	var eventType byte
	if isPress {
		eventType = xproto.ButtonPress
	} else {
		eventType = xproto.ButtonRelease
	}

	// 最后一个参数 0 是 delay，通常设为 0
	xtest.FakeInput(ic.conn, eventType, button, 0, ic.root, 0, 0, 0)
}

// KeyPress 模拟键盘按键
// keycode: X11 的按键编码 (注意：不是 ASCII，需要转换)
func (ic *InputController) KeyEvent(keycode byte, isPress bool) {
	var eventType byte
	if isPress {
		eventType = xproto.KeyPress
	} else {
		eventType = xproto.KeyRelease
	}
	xtest.FakeInput(ic.conn, eventType, keycode, 0, ic.root, 0, 0, 0)
}

// 请添加到 InputController 的方法中

func (ic *InputController) HandleMouseEvent(action byte, x, y int16, buttons uint32) {
	// 1. 移动鼠标 (无论 Move/Down/Up 都需要先移动到位)
	ic.MoveMouse(x, y)

	// 动作枚举 (你的定义)
	const (
		ActionMove = 0
		ActionDown = 1
		ActionUp   = 2
	)

	// 如果只是移动，做完 MoveMouse 就结束了
	if action == ActionMove {
		return
	}

	// 2. 解析按键
	// 将 Web 的掩码映射到 X11 的按键号
	var x11Button byte

	// 你的常量定义：BUTTON_PRIMARY = 1, SECONDARY = 2, TERTIARY = 4
	if buttons&1 != 0 { // BUTTON_PRIMARY
		x11Button = 1 // X11 左键
	} else if buttons&2 != 0 { // BUTTON_SECONDARY
		x11Button = 3 // X11 右键 (注意映射)
	} else if buttons&4 != 0 { // BUTTON_TERTIARY
		x11Button = 2 // X11 中键
	} else {
		// 默认左键 (防止空值)
		x11Button = 1
	}

	// 3. 执行点击
	isPress := (action == ActionDown)
	ic.MouseEvent(x11Button, isPress)

	// log.Printf("Mouse: Action=%d, X=%d, Y=%d, Btn=%d", action, x, y, x11Button)
}
