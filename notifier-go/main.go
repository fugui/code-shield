package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/co"
)

var (
	mainWnd   *ui.Main
	chkAuto   *ui.CheckBox
	btnBatch  *ui.Button
	btnClear  *ui.Button
	btnQuit   *ui.Button
	lstDrafts *ui.ListView
	txtLog    *ui.Edit
)

func main() {
	runtime.LockOSThread()

	err := initDB()
	if err != nil {
		panic("Database Initialization failed: " + err.Error())
	}

	mainWnd = ui.NewMain(
		ui.OptsMain().
			Title("Code-Shield Notifier").
			Size(600, 500),
	)

	setupControls()
	setupEvents()

	go StartHTTPServer()

	mainWnd.RunAsMain()
}

func setupControls() {
	chkAuto = ui.NewCheckBox(mainWnd, ui.OptsCheckBox().
		Text("自动发送").
		Position(10, 10).
		Size(80, 20))

	btnBatch = ui.NewButton(mainWnd, ui.OptsButton().
		Text("批量发送等待中的邮件").
		Position(100, 8).
		Width(150).Height(24))

	btnClear = ui.NewButton(mainWnd, ui.OptsButton().
		Text("清空全部记录").
		Position(260, 8).
		Width(100).Height(24))

	btnQuit = ui.NewButton(mainWnd, ui.OptsButton().
		Text("退出程序").
		Position(490, 8).
		Width(100).Height(24))

	lstDrafts = ui.NewListView(mainWnd, ui.OptsListView().
		Position(10, 40).
		Size(580, 290).
		Column("状态", 60).
		Column("收件人", 150).
		Column("主题", 220).
		Column("时间", 120))

	txtLog = ui.NewEdit(mainWnd, ui.OptsEdit().
		Position(10, 340).
		Width(580).Height(150).
		CtrlStyle(co.ES_MULTILINE | co.ES_READONLY | co.ES_AUTOVSCROLL | co.ES_NOHIDESEL).
		WndStyle(co.WS_CHILD | co.WS_VISIBLE | co.WS_TABSTOP | co.WS_GROUP | co.WS_VSCROLL))
}

func setupEvents() {
	mainWnd.On().WmCreate(func(p ui.WmCreate) int {
		if GetAutoSend() {
			chkAuto.SetCheck(true)
		} else {
			chkAuto.SetCheck(false)
		}
		
		RefreshDraftsUI()
		return 0
	})

	chkAuto.On().BnClicked(func() {
		state := chkAuto.IsChecked()
		SetAutoSend(state)
		if state {
			LogMessage("Auto-send enabled.")
		} else {
			LogMessage("Auto-send disabled.")
		}
	})

	btnBatch.On().BnClicked(func() {
		SendAllPendingDrafts()
	})

	btnClear.On().BnClicked(func() {
		drafts, _ := GetAllDrafts()
		for _, d := range drafts {
			DeleteDraft(d.ID)
		}
		RefreshDraftsUI()
		LogMessage("All drafts cleared from database.")
	})

	btnQuit.On().BnClicked(func() {
		mainWnd.Hwnd().PostMessage(co.WM_DESTROY, 0, 0)
	})

	// To keep things simple and functional without SysTray, we do not hide window on minimize
	// The user can keep it in the taskbar or minimize it normally.
}

func LogMessage(msg string) {
	fmt.Println(msg)
	if mainWnd == nil || mainWnd.Hwnd() == 0 {
		return
	}
	mainWnd.UiThread(func() {
		t := time.Now().Format("15:04:05")
		current := txtLog.Text()
		
		if len(current) > 10000 {
			current = current[:10000]
		}
		
		txtLog.SetText(fmt.Sprintf("[%s] %s\r\n%s", t, msg, current))
	})
}

func RefreshDraftsUI() {
	if mainWnd == nil || mainWnd.Hwnd() == 0 {
		return
	}
	mainWnd.UiThread(func() {
		drafts, err := GetAllDrafts()
		if err != nil {
			return
		}
		lstDrafts.DeleteAllItems()
		for _, d := range drafts {
			createdAtStr := d.CreatedAt.Format("01-02 15:04:05")
			lstDrafts.AddItem(d.Status, d.To, d.Subject, createdAtStr)
		}
	})
}
