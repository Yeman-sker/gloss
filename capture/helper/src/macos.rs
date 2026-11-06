//! macOS: Accessibility first, a simulated Cmd+C when an app offers none.
//! Every Create or Copy is held by a Cf, which releases it. AppKit (the
//! clipboard, an app's bundle id) is reached through the Objective-C
//! runtime, with no crate.
use crate::Fail;
use std::collections::{HashMap, HashSet};
use std::ffi::{c_char, c_void, CStr, CString};
use std::ptr::null_mut;
use std::time::{Duration, Instant};

type CFRef = *const c_void;
type Id = *mut c_void;
type Sel = *const c_void;

#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
struct CFRange {
    location: isize,
    length: isize,
}

#[link(name = "ApplicationServices", kind = "framework")]
extern "C" {
    fn AXIsProcessTrusted() -> bool;
    fn AXUIElementCreateSystemWide() -> CFRef;
    fn AXUIElementCreateApplication(pid: i32) -> CFRef;
    fn AXUIElementCopyAttributeValue(el: CFRef, attr: CFRef, out: *mut CFRef) -> i32;
    fn AXUIElementCopyParameterizedAttributeValue(el: CFRef, attr: CFRef, param: CFRef, out: *mut CFRef) -> i32;
    fn AXUIElementIsAttributeSettable(el: CFRef, attr: CFRef, out: *mut u8) -> i32;
    fn AXUIElementSetAttributeValue(el: CFRef, attr: CFRef, value: CFRef) -> i32;
    fn AXUIElementGetPid(el: CFRef, pid: *mut i32) -> i32;
    fn AXUIElementSetMessagingTimeout(el: CFRef, seconds: f32) -> i32;
    fn AXValueCreate(kind: u32, value: *const c_void) -> CFRef;
    fn AXValueGetValue(v: CFRef, kind: u32, out: *mut c_void) -> bool;
    fn CGEventSourceCreate(state: i32) -> CFRef;
    fn CGEventCreateKeyboardEvent(src: CFRef, key: u16, down: bool) -> CFRef;
    fn CGEventSetFlags(ev: CFRef, flags: u64);
    fn CGEventPostToPid(pid: i32, ev: CFRef);
    fn CGEventSourceButtonState(state: i32, button: u32) -> bool;
    fn CGEventSourceFlagsState(state: i32) -> u64;
    fn CGWindowListCopyWindowInfo(option: u32, relative_to: u32) -> CFRef;
}

#[link(name = "CoreFoundation", kind = "framework")]
extern "C" {
    static kCFBooleanTrue: CFRef;
    fn CFStringCreateWithCString(a: CFRef, s: *const c_char, enc: u32) -> CFRef;
    fn CFStringGetCString(s: CFRef, buf: *mut c_char, len: isize, enc: u32) -> bool;
    fn CFStringGetLength(s: CFRef) -> isize;
    fn CFGetTypeID(r: CFRef) -> usize;
    fn CFStringGetTypeID() -> usize;
    fn CFNumberGetTypeID() -> usize;
    fn CFNumberGetValue(n: CFRef, kind: isize, out: *mut c_void) -> bool;
    fn CFArrayGetCount(a: CFRef) -> isize;
    fn CFArrayGetValueAtIndex(a: CFRef, i: isize) -> CFRef;
    fn CFDictionaryGetValue(d: CFRef, key: CFRef) -> CFRef;
    fn CFEqual(a: CFRef, b: CFRef) -> bool;
    fn CFRetain(r: CFRef) -> CFRef;
    fn CFRelease(r: CFRef);
}

#[link(name = "AppKit", kind = "framework")]
extern "C" {}

#[link(name = "objc")]
extern "C" {
    fn objc_getClass(name: *const c_char) -> Id;
    fn sel_registerName(name: *const c_char) -> Sel;
    fn objc_msgSend();
    fn objc_autoreleasePoolPush() -> *mut c_void;
    fn objc_autoreleasePoolPop(pool: *mut c_void);
}

const UTF8: u32 = 0x0800_0100;
const AX_RANGE: u32 = 4;
const CF_SINT64: isize = 4;
const FLAG_SHIFT: u64 = 1 << 17;
const FLAG_CTRL: u64 = 1 << 18;
const FLAG_ALT: u64 = 1 << 19;
const FLAG_CMD: u64 = 1 << 20;
const KEY_C: u16 = 8;
const KEY_V: u16 = 9;
const KEY_CMD: u16 = 55;
const STATE_PRIVATE: i32 = -1;
const STATE_HID: i32 = 1;
const ON_SCREEN_ONLY: u32 = 1;
const EXCLUDE_DESKTOP: u32 = 16;
/// How far around the selection to read, in UTF-16 units each way: enough
/// for the sentence or two that explain a word, never the whole document.
const AROUND: isize = 300;
/// Above this size the whole value of a field is not read for its context.
const MAX_VALUE: isize = 20_000;

pub fn button_down() -> bool {
    unsafe { CGEventSourceButtonState(0, 0) }
}

pub fn trusted() -> bool {
    unsafe { AXIsProcessTrusted() }
}

/// An owned Core Foundation object, released when dropped.
struct Cf(CFRef);

impl Drop for Cf {
    fn drop(&mut self) {
        if !self.0.is_null() {
            unsafe { CFRelease(self.0) }
        }
    }
}

impl Cf {
    fn retain(r: CFRef) -> Cf {
        Cf(unsafe { CFRetain(r) })
    }
}

fn cfstr(s: &str) -> Cf {
    let c = CString::new(s).unwrap_or_default();
    Cf(unsafe { CFStringCreateWithCString(null_mut(), c.as_ptr(), UTF8) })
}

fn attr(el: CFRef, name: &str) -> Result<Cf, i32> {
    let key = cfstr(name);
    let mut out: CFRef = null_mut();
    let err = unsafe { AXUIElementCopyAttributeValue(el, key.0, &mut out) };
    if err == 0 && !out.is_null() {
        Ok(Cf(out))
    } else {
        if !out.is_null() {
            drop(Cf(out));
        }
        Err(err)
    }
}

fn param_attr(el: CFRef, name: &str, param: CFRef) -> Option<Cf> {
    let key = cfstr(name);
    let mut out: CFRef = null_mut();
    let err = unsafe { AXUIElementCopyParameterizedAttributeValue(el, key.0, param, &mut out) };
    if err == 0 && !out.is_null() {
        Some(Cf(out))
    } else {
        if !out.is_null() {
            drop(Cf(out));
        }
        None
    }
}

fn settable(el: CFRef, name: &str) -> Option<bool> {
    let key = cfstr(name);
    let mut out = 0u8;
    let err = unsafe { AXUIElementIsAttributeSettable(el, key.0, &mut out) };
    (err == 0).then_some(out != 0)
}

fn to_string(r: CFRef) -> Option<String> {
    unsafe {
        if r.is_null() || CFGetTypeID(r) != CFStringGetTypeID() {
            return None;
        }
        let cap = CFStringGetLength(r) * 4 + 1;
        let mut buf = vec![0 as c_char; cap as usize];
        if !CFStringGetCString(r, buf.as_mut_ptr(), cap, UTF8) {
            return None;
        }
        Some(CStr::from_ptr(buf.as_ptr()).to_string_lossy().into_owned())
    }
}

fn to_i64(r: CFRef) -> Option<i64> {
    unsafe {
        if r.is_null() || CFGetTypeID(r) != CFNumberGetTypeID() {
            return None;
        }
        let mut v = 0i64;
        CFNumberGetValue(r, CF_SINT64, &mut v as *mut i64 as *mut c_void).then_some(v)
    }
}

fn string_attr(el: CFRef, name: &str) -> Option<String> {
    attr(el, name).ok().and_then(|v| to_string(v.0))
}

fn range_attr(el: CFRef) -> Option<CFRange> {
    let v = attr(el, "AXSelectedTextRange").ok()?;
    let mut r = CFRange { location: 0, length: 0 };
    unsafe { AXValueGetValue(v.0, AX_RANGE, &mut r as *mut CFRange as *mut c_void) }.then_some(r)
}

fn string_for_range(el: CFRef, r: CFRange) -> Option<String> {
    if r.length <= 0 {
        return Some(String::new());
    }
    let v = Cf(unsafe { AXValueCreate(AX_RANGE, &r as *const CFRange as *const c_void) });
    param_attr(el, "AXStringForRange", v.0).and_then(|s| to_string(s.0))
}

// --- Objective-C, through objc_msgSend cast to each signature it needs.

macro_rules! msg {
    ($obj:expr, $sel:expr $(, $arg:expr => $t:ty)* ; $ret:ty) => {{
        let f: extern "C" fn(Id, Sel $(, $t)*) -> $ret = std::mem::transmute(objc_msgSend as *const ());
        f($obj as Id, sel($sel) $(, $arg)*)
    }};
}

fn sel(name: &str) -> Sel {
    let c = CString::new(name).unwrap_or_default();
    unsafe { sel_registerName(c.as_ptr()) }
}

fn class(name: &str) -> Id {
    let c = CString::new(name).unwrap_or_default();
    unsafe { objc_getClass(c.as_ptr()) }
}

fn ns_to_string(s: Id) -> Option<String> {
    if s.is_null() {
        return None;
    }
    let p: *const c_char = unsafe { msg!(s, "UTF8String"; *const c_char) };
    (!p.is_null()).then(|| unsafe { CStr::from_ptr(p) }.to_string_lossy().into_owned())
}

/// Drains what AppKit autoreleases while it lives.
struct Pool(*mut c_void);

impl Pool {
    fn new() -> Pool {
        Pool(unsafe { objc_autoreleasePoolPush() })
    }
}

impl Drop for Pool {
    fn drop(&mut self) {
        unsafe { objc_autoreleasePoolPop(self.0) }
    }
}

fn bundle_id(pid: i32) -> String {
    unsafe {
        let app: Id = msg!(class("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", pid => i32; Id);
        if app.is_null() {
            return String::new();
        }
        ns_to_string(msg!(app, "bundleIdentifier"; Id)).unwrap_or_default()
    }
}

// --- The clipboard, every item and every type of it.

fn pasteboard() -> Id {
    unsafe { msg!(class("NSPasteboard"), "generalPasteboard"; Id) }
}

fn change_count() -> i64 {
    unsafe { msg!(pasteboard(), "changeCount"; i64) }
}

/// What the clipboard held: each item's types and their data.
struct Snapshot {
    items: Vec<Vec<(String, Vec<u8>)>>,
}

impl Snapshot {
    fn take() -> Snapshot {
        let mut items = Vec::new();
        unsafe {
            let arr: Id = msg!(pasteboard(), "pasteboardItems"; Id);
            let n = if arr.is_null() { 0 } else { msg!(arr, "count"; usize) };
            for i in 0..n {
                let item: Id = msg!(arr, "objectAtIndex:", i => usize; Id);
                let types: Id = msg!(item, "types"; Id);
                let tn = if types.is_null() { 0 } else { msg!(types, "count"; usize) };
                let mut kept = Vec::new();
                for j in 0..tn {
                    let ty: Id = msg!(types, "objectAtIndex:", j => usize; Id);
                    let data: Id = msg!(item, "dataForType:", ty => Id; Id);
                    let Some(name) = ns_to_string(ty) else { continue };
                    if data.is_null() {
                        continue; // a promise its owner no longer keeps
                    }
                    let len: usize = msg!(data, "length"; usize);
                    let bytes: *const u8 = msg!(data, "bytes"; *const u8);
                    let v = if len == 0 || bytes.is_null() { Vec::new() } else { std::slice::from_raw_parts(bytes, len).to_vec() };
                    kept.push((name, v));
                }
                items.push(kept);
            }
        }
        Snapshot { items }
    }

    fn restore(&self) {
        unsafe {
            let pb = pasteboard();
            msg!(pb, "clearContents"; i64);
            if self.items.is_empty() {
                return;
            }
            let mut objs: Vec<Id> = Vec::new();
            for item in &self.items {
                let it: Id = msg!(class("NSPasteboardItem"), "new"; Id);
                for (ty, data) in item {
                    let nsdata: Id = msg!(class("NSData"), "dataWithBytes:length:", data.as_ptr() as *const c_void => *const c_void, data.len() => usize; Id);
                    let t = cfstr(ty);
                    msg!(it, "setData:forType:", nsdata => Id, t.0 as Id => Id; bool);
                }
                objs.push(it);
            }
            let arr: Id = msg!(class("NSArray"), "arrayWithObjects:count:", objs.as_ptr() => *const Id, objs.len() => usize; Id);
            msg!(pb, "writeObjects:", arr => Id; bool);
            for it in objs {
                msg!(it, "release"; ());
            }
        }
    }
}

fn clipboard_text() -> Option<String> {
    let ty = cfstr("public.utf8-plain-text");
    unsafe { ns_to_string(msg!(pasteboard(), "stringForType:", ty.0 as Id => Id; Id)) }
}

fn set_clipboard_text(s: &str) {
    let ty = cfstr("public.utf8-plain-text");
    let text = cfstr(s);
    unsafe {
        let pb = pasteboard();
        msg!(pb, "clearContents"; i64);
        msg!(pb, "setString:forType:", text.0 as Id => Id, ty.0 as Id => Id; bool);
    }
}

// --- Keys.

/// waitForKeysUp lets the user lift the shortcut's keys: an app that reads
/// the keyboard's state would take Cmd+C with Option and Shift held for
/// another command.
fn wait_for_keys_up() {
    let start = Instant::now();
    while start.elapsed() < Duration::from_millis(400) {
        let flags = unsafe { CGEventSourceFlagsState(STATE_HID) };
        if flags & (FLAG_SHIFT | FLAG_CTRL | FLAG_ALT | FLAG_CMD) == 0 {
            return;
        }
        std::thread::sleep(Duration::from_millis(5));
    }
}

// AX errors that mean the call was refused before anything was written.
const AX_ERR_ILLEGAL_ARGUMENT: i32 = -25201;
const AX_ERR_ATTRIBUTE_UNSUPPORTED: i32 = -25205;

/// press types Cmd+key into the app pid as a keyboard does: Cmd down, the
/// key down and up, Cmd up. The events go to that process alone, not to
/// whatever window has the keyboard: a click on Gloss's panel can make the
/// panel the key window without activating Gloss, and a global Cmd+V or
/// Cmd+C then went to the panel (measured, TextEdit). They come from a
/// private source, so their flags are their own and not those of keys the
/// user holds, and the system's key state is left alone (a global post
/// without the Cmd up left the HID flags believing Cmd was held).
fn press(pid: i32, key: u16) {
    unsafe {
        let src = Cf(CGEventSourceCreate(STATE_PRIVATE));
        for (code, down, flags) in [(KEY_CMD, true, FLAG_CMD), (key, true, FLAG_CMD), (key, false, FLAG_CMD), (KEY_CMD, false, 0)] {
            let ev = Cf(CGEventCreateKeyboardEvent(src.0, code, down));
            CGEventSetFlags(ev.0, flags);
            CGEventPostToPid(pid, ev.0);
        }
    }
}

/// copy_selection asks the app to copy, reads what it copied, and puts back
/// every item and type the clipboard held, unless something else wrote it
/// in the meantime: then that newer content stays.
fn copy_selection(pid: i32) -> Result<String, Fail> {
    // Wait for the keys first: what the clipboard holds after this is the
    // user's, and only a change after Cmd+C is the copy.
    wait_for_keys_up();
    let snapshot = Snapshot::take();
    let before = change_count();
    press(pid, KEY_C);
    let start = Instant::now();
    let mut copied = None;
    while start.elapsed() < Duration::from_millis(600) {
        let now = change_count();
        if now != before {
            // An app may clear the clipboard and write it in two steps.
            std::thread::sleep(Duration::from_millis(15));
            copied = Some((change_count(), clipboard_text()));
            break;
        }
        std::thread::sleep(Duration::from_millis(5));
    }
    let Some((count, text)) = copied else {
        // Nothing was copied, so there is nothing to put back.
        return Err(Fail::new("no_selection", "copy produced nothing"));
    };
    if change_count() == count {
        snapshot.restore();
    }
    text.filter(|t| !t.trim().is_empty()).ok_or_else(|| Fail::new("no_selection", "copy produced no text"))
}

// --- Where the selection is.

/// front_pid is the app with the focus: the system-wide element names it,
/// or else the owner of the frontmost normal window. Neither starts a
/// process, as lsappinfo did.
fn front_pid(system: CFRef) -> Option<i32> {
    let me = std::process::id() as i64;
    let parent = unsafe { libc_getppid() } as i64;
    // A click on Gloss's panel can make Gloss the focused application, though
    // the panel never activates: the app the user works in is then the one
    // under it.
    if let Ok(app) = attr(system, "AXFocusedApplication") {
        let mut pid = 0;
        if unsafe { AXUIElementGetPid(app.0, &mut pid) } == 0 && pid > 0 && pid as i64 != me && pid as i64 != parent {
            return Some(pid);
        }
    }
    unsafe {
        let list = Cf(CGWindowListCopyWindowInfo(ON_SCREEN_ONLY | EXCLUDE_DESKTOP, 0));
        if list.0.is_null() {
            return None;
        }
        let (layer_key, pid_key) = (cfstr("kCGWindowLayer"), cfstr("kCGWindowOwnerPID"));
        for i in 0..CFArrayGetCount(list.0) {
            let w = CFArrayGetValueAtIndex(list.0, i);
            if to_i64(CFDictionaryGetValue(w, layer_key.0)) != Some(0) {
                continue;
            }
            match to_i64(CFDictionaryGetValue(w, pid_key.0)) {
                Some(p) if p != me && p != parent => return Some(p as i32),
                _ => {}
            }
        }
    }
    None
}

extern "C" {
    #[link_name = "getppid"]
    fn libc_getppid() -> i32;
}

/// A place a selection was read, kept to check it before writing back.
struct Target {
    pid: i32,
    element: Cf,
    text: String,
    range: Option<CFRange>,
    editable: &'static str,
}

pub struct State {
    targets: HashMap<u64, Target>,
    order: Vec<u64>,
    next: u64,
    /// apps asked to build their accessibility tree, as Electron and
    /// Chromium apps do only when asked.
    woken: HashSet<i32>,
}

impl State {
    pub fn new() -> State {
        // An app that hangs would hold every request up for six seconds.
        unsafe {
            let system = Cf(AXUIElementCreateSystemWide());
            AXUIElementSetMessagingTimeout(system.0, 0.8);
        }
        // Tokens carry the process's pid: one a Gloss kept from before a
        // restart then names nothing here, rather than another target.
        // pids stay below 2^17, so tokens stay below 2^49, exact in the f64
        // the request parser reads numbers into.
        let next = ((std::process::id() as u64) << 32) | 1;
        State { targets: HashMap::new(), order: Vec::new(), next, woken: HashSet::new() }
    }

    fn remember(&mut self, t: Target) -> u64 {
        let token = self.next;
        self.next += 1;
        self.targets.insert(token, t);
        self.order.push(token);
        while self.order.len() > 8 {
            let old = self.order.remove(0);
            self.targets.remove(&old);
        }
        token
    }
}

pub fn forget(state: &mut State, token: u64) {
    state.targets.remove(&token);
    state.order.retain(|t| *t != token);
}

pub struct Selection {
    pub text: String,
    pub method: &'static str,
    pub app: String,
    pub title: String,
    pub editable: &'static str,
    pub before: String,
    pub after: String,
    pub token: u64,
    pub ctx_us: u128,
}

/// wake asks an app for its accessibility tree, which Electron and Chromium
/// build only for assistive technology. It reports whether this is the
/// first time, when the tree may not be there yet.
fn wake(state: &mut State, app: CFRef, pid: i32) -> bool {
    if !state.woken.insert(pid) {
        return false;
    }
    let key = cfstr("AXManualAccessibility");
    let err = unsafe { AXUIElementSetAttributeValue(app, key.0, kCFBooleanTrue) };
    err == 0
}

fn editable_of(el: CFRef) -> &'static str {
    match settable(el, "AXValue").or_else(|| settable(el, "AXSelectedText")) {
        Some(true) => "yes",
        Some(false) => "no",
        None => "unknown",
    }
}

/// around reads up to AROUND units on each side of the selection.
fn around(el: CFRef, r: CFRange) -> (String, String) {
    let start = (r.location - AROUND).max(0);
    let before = string_for_range(el, CFRange { location: start, length: r.location - start });
    let after = string_for_range(el, CFRange { location: r.location + r.length, length: AROUND });
    if before.is_some() || after.is_some() {
        // AXStringForRange fails past the end of the text in some apps.
        let after = after.or_else(|| {
            let n = attr(el, "AXNumberOfCharacters").ok().and_then(|v| to_i64(v.0))? as isize;
            let from = r.location + r.length;
            string_for_range(el, CFRange { location: from, length: (n - from).clamp(0, AROUND) })
        });
        return (before.unwrap_or_default(), after.unwrap_or_default());
    }
    // A small field without ranged reads: take its value and cut it.
    let n = attr(el, "AXNumberOfCharacters").ok().and_then(|v| to_i64(v.0)).unwrap_or(isize::MAX as i64) as isize;
    if n > MAX_VALUE {
        return (String::new(), String::new());
    }
    let Some(value) = string_attr(el, "AXValue") else { return (String::new(), String::new()) };
    let units: Vec<u16> = value.encode_utf16().collect();
    let at = (r.location.max(0) as usize).min(units.len());
    let end = ((r.location + r.length).max(0) as usize).min(units.len());
    let from = at.saturating_sub(AROUND as usize);
    let to = (end + AROUND as usize).min(units.len());
    (String::from_utf16_lossy(&units[from..at]), String::from_utf16_lossy(&units[end..to]))
}

pub fn selection(state: &mut State, copy: bool, copy_only: bool) -> Result<Selection, Fail> {
    if !trusted() {
        return Err(Fail::new("permission", "no Accessibility access"));
    }
    let _pool = Pool::new();
    let system = Cf(unsafe { AXUIElementCreateSystemWide() });
    let pid = front_pid(system.0).ok_or_else(|| Fail::new("no_selection", "no frontmost app"))?;
    let app = Cf(unsafe { AXUIElementCreateApplication(pid) });
    let first = wake(state, app.0, pid);
    let mut focused = attr(app.0, "AXFocusedUIElement").ok();
    let mut text = focused.as_ref().and_then(|f| string_attr(f.0, "AXSelectedText")).filter(|t| !t.is_empty());
    if copy_only {
        text = None;
    } else if text.is_none() && first {
        std::thread::sleep(Duration::from_millis(80));
        focused = attr(app.0, "AXFocusedUIElement").ok();
        text = focused.as_ref().and_then(|f| string_attr(f.0, "AXSelectedText")).filter(|t| !t.is_empty());
    }

    let ctx_start = Instant::now();
    let bundle = bundle_id(pid);
    let title = attr(app.0, "AXFocusedWindow").ok().and_then(|w| string_attr(w.0, "AXTitle")).unwrap_or_default();
    let mut out = Selection {
        text: String::new(),
        method: "ax",
        app: bundle,
        title,
        editable: "unknown",
        before: String::new(),
        after: String::new(),
        token: 0,
        ctx_us: 0,
    };

    if let (Some(t), Some(el)) = (text, focused.as_ref()) {
        out.editable = editable_of(el.0);
        let range = range_attr(el.0);
        if let Some(r) = range {
            (out.before, out.after) = around(el.0, r);
        }
        out.ctx_us = ctx_start.elapsed().as_micros();
        out.token = state.remember(Target { pid, element: Cf::retain(el.0), text: t.clone(), range, editable: out.editable });
        out.text = t;
        return Ok(out);
    }
    if !copy && !copy_only {
        return Err(Fail::new("no_selection", "no AXSelectedText"));
    }
    if let Some(el) = focused.as_ref() {
        out.editable = editable_of(el.0);
    }
    out.ctx_us = ctx_start.elapsed().as_micros();
    out.text = copy_selection(pid)?;
    out.method = "copy";
    if let Some(el) = focused {
        let editable = out.editable;
        out.token = state.remember(Target { pid, element: el, text: out.text.clone(), range: None, editable });
    }
    Ok(out)
}

/// replace writes text over the selection read under token, after checking
/// that the same app, the same element and the same selected text are still
/// there. It returns how it wrote and whether it saw the text land. It
/// never writes twice: when a write cannot be confirmed it says so.
pub fn replace(state: &mut State, token: u64, text: &str, method: &str) -> Result<(&'static str, bool), Fail> {
    if !trusted() {
        return Err(Fail::new("permission", "no Accessibility access"));
    }
    let _pool = Pool::new();
    let t = state.targets.get(&token).ok_or_else(|| Fail::new("expired", "the selection is no longer known"))?;
    if t.editable != "yes" {
        return Err(Fail::new("not_editable", "the selection is not in an editable field"));
    }
    // Wait for the keys before checking, never between the check and the
    // paste: Cmd+V goes to whatever app has the focus when it is sent.
    wait_for_keys_up();
    let system = Cf(unsafe { AXUIElementCreateSystemWide() });
    if front_pid(system.0) != Some(t.pid) {
        return Err(Fail::new("changed", "another app is in front"));
    }
    let app = Cf(unsafe { AXUIElementCreateApplication(t.pid) });
    let focused = attr(app.0, "AXFocusedUIElement").map_err(|_| Fail::new("changed", "the field lost the focus"))?;
    if !unsafe { CFEqual(focused.0, t.element.0) } {
        return Err(Fail::new("changed", "the focus moved to another field"));
    }
    let now = string_attr(focused.0, "AXSelectedText").ok_or_else(|| Fail::new("unverifiable", "the selection cannot be read"))?;
    if now != t.text {
        return Err(Fail::new("changed", "the selection changed"));
    }
    let range = range_attr(focused.0);
    if t.range.is_some() && range != t.range {
        return Err(Fail::new("changed", "the selection moved"));
    }
    let written = CFRange { location: range.map_or(0, |r| r.location), length: text.encode_utf16().count() as isize };
    // untouched: the selection is still the one read, so nothing was written.
    let untouched = || string_attr(focused.0, "AXSelectedText").as_deref() == Some(t.text.as_str()) && range_attr(focused.0) == range;
    // landed: the text is where the selection began, and the selection is
    // no longer the one read. Text that begins the original ("hello" over
    // "hello world") would read as landed without the second test.
    let landed = || range.is_some() && string_for_range(focused.0, written).as_deref() == Some(text) && (text == t.text || !untouched());

    // AX writes to the element itself, wherever the keyboard is. "auto"
    // tries it first where the field says it can take it, and pastes only
    // when AX surely wrote nothing.
    let try_ax = method == "ax" || method == "auto" && settable(focused.0, "AXSelectedText") == Some(true);
    if try_ax {
        let key = cfstr("AXSelectedText");
        let value = cfstr(text);
        let err = unsafe { AXUIElementSetAttributeValue(focused.0, key.0, value.0) };
        // An error may come after the app wrote (a timeout, say): what the
        // field shows decides, not what AX answered.
        if landed() {
            forget(state, token);
            return Ok(("ax", true));
        }
        // An unchanged selection does not prove nothing was written: an app
        // may write and keep reporting the old selection. Pasting is safe
        // only when AX refused outright, before touching anything.
        let refused = err == AX_ERR_ATTRIBUTE_UNSUPPORTED || err == AX_ERR_ILLEGAL_ARGUMENT;
        if method == "ax" && refused && untouched() {
            return Err(Fail::new("failed", format!("AX refused the text ({err})")));
        }
        if method == "ax" || !refused || !untouched() {
            forget(state, token);
            return Ok(("ax", false));
        }
    }

    let snapshot = Snapshot::take();
    set_clipboard_text(text);
    let ours = change_count();
    // The last check, right before the key: the same app and field still
    // have the focus, with the same selection.
    let still = front_pid(system.0) == Some(t.pid)
        && attr(app.0, "AXFocusedUIElement").map_or(false, |f| unsafe { CFEqual(f.0, t.element.0) })
        && untouched();
    if !still {
        if change_count() == ours {
            snapshot.restore();
        }
        return Err(Fail::new("changed", "the focus moved before writing"));
    }
    press(t.pid, KEY_V);
    // The app reads the clipboard when it handles the key: wait until the
    // text shows, or long enough, before giving the clipboard back.
    let start = Instant::now();
    let mut verified = false;
    while start.elapsed() < Duration::from_millis(900) {
        std::thread::sleep(Duration::from_millis(30));
        if landed() {
            verified = true;
            break;
        }
    }
    if !verified {
        std::thread::sleep(Duration::from_millis(100));
    }
    if change_count() == ours {
        snapshot.restore();
    }
    forget(state, token);
    Ok(("paste", verified))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cf_strings_round_trip() {
        let s = cfstr("取词 gloss");
        assert_eq!(to_string(s.0).as_deref(), Some("取词 gloss"));
    }

    #[test]
    fn targets_are_bounded() {
        let mut st = State { targets: HashMap::new(), order: Vec::new(), next: 1, woken: HashSet::new() };
        for _ in 0..20 {
            let el = cfstr("x");
            st.remember(Target { pid: 1, element: Cf::retain(el.0), text: String::new(), range: None, editable: "no" });
        }
        assert_eq!(st.targets.len(), 8);
        assert!(st.targets.contains_key(&20) && !st.targets.contains_key(&12));
        forget(&mut st, 20);
        assert_eq!(st.targets.len(), 7);
    }
}
