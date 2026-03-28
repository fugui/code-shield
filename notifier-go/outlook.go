package main

import (
	"fmt"
	"os"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func SendDraft(draft DraftEntity) {
	UpdateDraftStatus(draft.ID, "发送中")
	RefreshDraftsUI()

	err := func() error {
		ole.CoInitialize(0)
		defer ole.CoUninitialize()

		unknown, err := oleutil.CreateObject("Outlook.Application")
		if err != nil {
			return fmt.Errorf("could not create Outlook object: %w", err)
		}
		defer unknown.Release()

		outlook, err := unknown.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return fmt.Errorf("could not query IDispatch: %w", err)
		}
		defer outlook.Release()

		item, err := oleutil.CallMethod(outlook, "CreateItem", 0)
		if err != nil {
			return fmt.Errorf("could not CreateItem: %w", err)
		}
		mail := item.ToIDispatch()
		defer mail.Release()

		oleutil.PutProperty(mail, "To", draft.To)
		oleutil.PutProperty(mail, "CC", draft.CC)
		oleutil.PutProperty(mail, "Subject", draft.Subject)
		oleutil.PutProperty(mail, "HTMLBody", draft.HtmlBody)

		attachments, err := oleutil.GetProperty(mail, "Attachments")
		if err != nil {
			return fmt.Errorf("could not get Attachments: %w", err)
		}
		att := attachments.ToIDispatch()
		defer att.Release()

		if _, err := os.Stat(draft.PdfPath); err == nil {
			_, err = oleutil.CallMethod(att, "Add", draft.PdfPath)
			if err != nil {
				LogMessage(fmt.Sprintf("Warning: could not attach PDF %s: %v", draft.PdfPath, err))
			}
		} else {
			LogMessage(fmt.Sprintf("Warning: PDF not found at path %s", draft.PdfPath))
		}

		_, err = oleutil.CallMethod(mail, "Send")
		if err != nil {
			return fmt.Errorf("could not Send email: %w", err)
		}

		return nil
	}()

	if err != nil {
		LogMessage(fmt.Sprintf("Outlook COM Error for Draft ID %d: %v", draft.ID, err))
		UpdateDraftStatus(draft.ID, "失败")
	} else {
		UpdateDraftStatus(draft.ID, "已完成")
		LogMessage(fmt.Sprintf("Successfully sent Draft ID %d via Outlook", draft.ID))
		
		_ = os.Remove(draft.PdfPath)
	}

	RefreshDraftsUI()
}

func SendAllPendingDrafts() {
	drafts, err := GetAllDrafts()
	if err != nil {
		LogMessage(fmt.Sprintf("Failed to get drafts for batch sending: %v", err))
		return
	}
	
	count := 0
	for _, d := range drafts {
		if d.Status == "草稿" || d.Status == "失败" {
			count++
			go SendDraft(d)
		}
	}
	
	if count == 0 {
		LogMessage("No pending drafts to send.")
	} else {
		LogMessage(fmt.Sprintf("Started sending %d pending drafts at background...", count))
	}
}
