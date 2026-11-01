//! macOS: Accessibility first, simulated Cmd+C when an app (Chrome) offers none.
use std::ffi::{c_void, CStr, CString};
use std::io::Write;
use std::process::{Command, Stdio};
use std::ptr::null_mut;
use std::time::{Duration, Instant};

pub const API: &str = "ax";

type CFRef = *const c_void;

#[link(name = "ApplicationServices", kind = "framework")]
extern "C" {
    fn AXIsProcessTrusted() -> bool;
    fn AXUIElementCreateSystemWide() -> CFRef;
    fn AXUIElementCreateApplication(pid: i32) -> CFRef;
    fn AXUIElementCopyAttributeValue(el: CFRef, attr: CFRef, out: *mut CFRef) -> i32;
    fn CGEventCreateKeyboardEvent(src: CFRef, key: u16, down: bool) -> CFRef;
    fn CGEventSetFlags(ev: CFRef, flags: u64);
    fn CGEventPost(tap: u32, ev: CFRef);
}

#[link(name = "CoreFoundation", kind = "framework")]
extern "C" {
    fn CFStringCreateWithCString(a: CFRef, s: *const i8, enc: u32) -> CFRef;
    fn CFStringGetCString(s: CFRef, buf: *mut i8, len: isize, enc: u32) -> bool;
    fn CFStringGetLength(s: CFRef) -> isize;
    fn CFGetTypeID(r: CFRef) -> usize;
    fn CFStringGetTypeID() -> usize;
    fn CFRelease(r: CFRef);
}

const UTF8: u32 = 0x0800_0100;
const FLAG_CMD: u64 = 1 << 20;
const KEY_C: u16 = 8;

pub fn trusted() -> bool {
    unsafe { AXIsProcessTrusted() }
}

fn cfstr(s: &str) -> CFRef {
    let c = CString::new(s).unwrap();
    unsafe { CFStringCreateWithCString(null_mut(), c.as_ptr(), UTF8) }
}

fn attr(el: CFRef, name: &str) -> Result<CFRef, i32> {
    let key = cfstr(name);
    let mut out: CFRef = null_mut();
    let err = unsafe { AXUIElementCopyAttributeValue(el, key, &mut out) };
    unsafe { CFRelease(key) };
    if err == 0 && !out.is_null() {
        Ok(out)
    } else {
        Err(err)
    }
}

fn to_string(r: CFRef) -> Option<String> {
    unsafe {
        if CFGetTypeID(r) != CFStringGetTypeID() {
            return None;
        }
        let cap = CFStringGetLength(r) * 4 + 1;
        let mut buf = vec![0i8; cap as usize];
        if !CFStringGetCString(r, buf.as_mut_ptr(), cap, UTF8) {
            return None;
        }
        Some(CStr::from_ptr(buf.as_ptr()).to_string_lossy().into_owned())
    }
}

pub fn read_selection() -> Result<String, String> {
    // The system-wide element answers -25204 on current macOS; the app's works.
    let system = unsafe { AXUIElementCreateSystemWide() };
    let focused = attr(system, "AXFocusedUIElement")
        .or_else(|sys_err| {
            let pid = front_pid().ok_or(sys_err)?;
            attr(unsafe { AXUIElementCreateApplication(pid) }, "AXFocusedUIElement")
        })
        .map_err(|e| format!("no focused element ({e})"))?;
    let value = attr(focused, "AXSelectedText").map_err(|e| format!("no AXSelectedText ({e})"))?;
    let text = to_string(value).ok_or("AXSelectedText is not a string")?;
    if text.is_empty() {
        Err("selection is empty".into())
    } else {
        Ok(text)
    }
}

// TODO: ask NSWorkspace rather than spawning lsappinfo, which costs ~20 ms.
fn front_pid() -> Option<i32> {
    let front = Command::new("lsappinfo").arg("front").output().ok()?;
    let asn = String::from_utf8_lossy(&front.stdout).trim().to_string();
    let info = Command::new("lsappinfo").args(["info", "-only", "pid", &asn]).output().ok()?;
    let out = String::from_utf8_lossy(&info.stdout).into_owned();
    out.split("pid = ").nth(1)?.split_whitespace().next()?.parse().ok()
}

fn pbpaste() -> String {
    Command::new("pbpaste")
        .env("LANG", "en_US.UTF-8")
        .output()
        .map(|o| String::from_utf8_lossy(&o.stdout).into_owned())
        .unwrap_or_default()
}

fn pbcopy(s: &str) {
    if let Ok(mut c) = Command::new("pbcopy").env("LANG", "en_US.UTF-8").stdin(Stdio::piped()).spawn() {
        let _ = c.stdin.take().unwrap().write_all(s.as_bytes());
        let _ = c.wait();
    }
}

fn press_copy() {
    for down in [true, false] {
        unsafe {
            let ev = CGEventCreateKeyboardEvent(null_mut(), KEY_C, down);
            CGEventSetFlags(ev, FLAG_CMD);
            CGEventPost(0, ev);
            CFRelease(ev);
        }
    }
}

// TODO: the restore keeps plain text only; it must keep every format.
pub fn copy_selection() -> Result<String, String> {
    let saved = pbpaste();
    let sentinel = format!("__gloss_{}__", std::process::id());
    pbcopy(&sentinel);
    press_copy();
    let start = Instant::now();
    let mut text = None;
    while start.elapsed() < Duration::from_millis(250) {
        let now = pbpaste();
        if now != sentinel {
            text = Some(now);
            break;
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    pbcopy(&saved);
    text.filter(|t| !t.is_empty()).ok_or_else(|| "copy produced nothing".into())
}
