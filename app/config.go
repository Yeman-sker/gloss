package app

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"

	"gloss/settings"
)

// configTemplate is the file the tray creates for the user to fill in. Its
// key is empty: the user pastes theirs, and the file stays theirs alone.
const configTemplate = `{
  "services": {
    "main": {
      "base_url": "https://api.openai.com/v1",
      "api_key": "",
      "model": ""
    }
  },
  "default_service": "main",
  "target_lang": "zh-Hans",
  "shortcut": "Alt+Shift+D",
  "memory": {
    "enabled": true,
    "retention_days": 90,
    "max_records": 5000
  },
  "routing": {
    "enabled": true,
    "jev_service": "",
    "wait_ms": 800,
    "min_confidence": 0.6,
    "send_window_title": false
  },
  "actions": ["translate", "explain", "prompt", "agent"]
}
`

// config is the config as last read. It is never nil.
func (s *shell) config() *settings.Config {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.cfg
}

// loadConfig reads the file. A file that is wrong keeps the switches as
// they were, and its error reaches the user when a feature runs.
func (s *shell) loadConfig() {
	cfg, err := settings.Read(s.cfgPath, s.descs)
	if info, statErr := os.Stat(s.cfgPath); statErr == nil {
		s.cfgStamp = info.ModTime()
	} else {
		s.cfgStamp = time.Time{}
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if err != nil && !errors.Is(err, settings.ErrMissing) && s.cfg != nil {
		s.cfgErr = err
		return
	}
	s.cfg, s.cfgErr = cfg, err
	if cfg.Migrated {
		log.Printf("config: old format, still read as it is")
	}
}

// watchConfig reads the file again when it changes, so that switches made
// by hand take effect without a restart.
func (s *shell) watchConfig() {
	for range time.Tick(2 * time.Second) {
		var stamp time.Time
		if info, err := os.Stat(s.cfgPath); err == nil {
			stamp = info.ModTime()
		}
		mygo.RunOnMain(func() {
			if !stamp.Equal(s.cfgStamp) {
				s.reload()
			}
		})
	}
}

// reload applies the file: the shortcut, the switches and memory.
func (s *shell) reload() {
	before := s.config()
	s.loadConfig()
	cfg := s.config()
	if s.cfgErr != nil {
		log.Printf("config: %v", s.cfgErr)
	} else {
		log.Printf("config: reloaded")
	}
	s.registerShortcut(cfg.Shortcut)
	if s.autoItem != nil {
		s.autoItem.SetChecked(cfg.Routing.Enabled)
		s.memItem.SetChecked(cfg.Memory.Enabled)
		s.loginItem.SetChecked(cfg.LaunchAtLogin)
	}
	if s.store != nil && s.store.Enabled() != cfg.Memory.Enabled {
		if err := s.store.SetEnabled(cfg.Memory.Enabled); err != nil {
			log.Printf("memory: %v", err)
		}
	}
	if before.LaunchAtLogin != cfg.LaunchAtLogin {
		s.applyLogin(cfg.LaunchAtLogin)
	}
}

func (s *shell) applyLogin(on bool) {
	if err := mygo.App.SetOpenAtLogin(on); err != nil {
		log.Printf("login item: %v", err)
		s.showNotice("登录时启动需要从打包的 Gloss.app 运行")
	}
}

// setOption is a tray switch: it changes the file, which is the one place
// settings live, and applies it.
func (s *shell) setOption(on bool, keys ...string) {
	if err := settings.Set(s.cfgPath, on, keys...); err != nil {
		log.Printf("config: %v", err)
		s.showNotice("无法修改配置文件")
		return
	}
	s.reload()
}

// openConfig opens the file in a text editor, creating it first.
func (s *shell) openConfig() {
	if _, err := os.Stat(s.cfgPath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(s.cfgPath), 0o700); err != nil {
			log.Printf("config: %v", err)
			return
		}
		if err := os.WriteFile(s.cfgPath, []byte(configTemplate), 0o600); err != nil {
			log.Printf("config: %v", err)
			return
		}
	}
	var err error
	if runtime.GOOS == "darwin" {
		err = exec.Command("open", "-t", s.cfgPath).Run()
	} else {
		err = mygo.Shell.OpenPath(s.cfgPath)
	}
	if err != nil {
		log.Printf("config: open: %v", err)
	}
}
