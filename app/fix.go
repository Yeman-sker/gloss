package app

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gloss/providers/openai"
	"gloss/settings"
)

// accessibilityPane is the Accessibility list of System Settings.
const accessibilityPane = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

// explain words a failed run for the user and says where it can be fixed:
// the settings page of what was refused, never the config file's keys.
func (s *shell) explain(err error) (msg, fix string, do func()) {
	open := func(page string) func() { return func() { s.openPrefs(page) } }
	var status *openai.StatusError
	var missing *settings.MissingError
	switch {
	case errors.Is(err, settings.ErrInvalid):
		return "配置文件有误，设置窗口里能看到哪一项不对", "打开设置", open(pageGeneral)
	case errors.Is(err, settings.ErrNoService), errors.Is(err, settings.ErrMissing):
		return "还没有设置模型服务", "设置模型服务", open(pageServices)
	case errors.As(err, &missing):
		return err.Error(), "设置模型服务", open(pageServices)
	case errors.As(err, &status):
		switch {
		case status.Code == 401 || status.Code == 403 || status.Code == 404 || status.Code == 400:
			return err.Error(), "检查模型服务", open(pageServices)
		}
		return err.Error(), "", nil
	case errors.Is(err, openai.ErrUnreachable):
		return "连不上模型服务，请检查网络和接口地址", "检查模型服务", open(pageServices)
	}
	return err.Error(), "", nil
}

// openAccessibility opens the Accessibility list in System Settings, after
// asking macOS to put Gloss in it.
func (s *shell) openAccessibility() {
	if s.helper != nil {
		go s.helper.AskTrust()
	}
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", accessibilityPane).Start()
	}
}

// brief takes the config file's path out of an error of it.
func brief(err error, path string) string {
	msg := err.Error()
	for _, prefix := range []string{"配置文件 " + path + " 中 ", "配置文件 " + path + " "} {
		msg = strings.Replace(msg, prefix, "", 1)
	}
	return msg
}

// reveal shows a file in the file manager.
func reveal(path string) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = exec.Command("open", "-R", path).Start()
	case "windows":
		err = exec.Command("explorer", "/select,", path).Start()
	default:
		err = exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
	if err != nil {
		log.Printf("reveal: %v", err)
	}
}
