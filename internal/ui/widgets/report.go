package widgets

import "github.com/patrache/masil/internal/ui"

// ReportDialog: reason list (spam|harassment|illegal|sexual|violence|other, Korean labels)
// + optional note (textinput, CA 5.3.7 rules), submits api.Report via ui.Do tag
// "core.report:<targetID>"; 201 => ToastSuccess "신고했어요", 409 already_exists =>
// ToastInfo "이미 신고한 대상이에요". targetType: user|message|post|story|guestbook_entry|space
// (api.md 5.21). label = what is shown as the target ("민지의 메시지").
func ReportDialog(targetType, targetID, label string) ui.Overlay {
	// TODO(core): Task 28
	return nil
}
