//! Reads, and writes back, the text selected in the frontmost app. One JSON
//! request per stdin line, one JSON reply per stdout line, with the
//! request's id so that the client can drop a late answer:
//!
//!   {"id":1,"cmd":"check"}                  {"id":1,"trusted":bool}
//!   {"id":1,"cmd":"check","prompt":true}    the same, and macOS's prompt when not
//!   {"id":2,"cmd":"button"}                 {"id":2,"down":bool}
//!   {"id":3,"cmd":"selection","copy":true}  {"id":3,"ok":true,"text":..,"method":"ax"|"copy","us":..,
//!                                            "app":..,"title":..,"editable":"yes"|"no"|"unknown",
//!                                            "before":..,"after":..,"token":..,"ctx_us":..}
//!   {"id":4,"cmd":"replace","token":7,"text":"..","method":"auto"|"paste"|"ax"}
//!                                           {"id":4,"ok":true,"method":..,"verified":bool,"us":..}
//!   {"id":5,"cmd":"forget","token":7}       {"id":5,"ok":true}
//!
//! A failure is {"id":..,"ok":false,"code":..,"error":..}. Codes: permission
//! (no Accessibility access), no_selection, changed (the target moved since
//! it was read), not_editable, unverifiable, expired, failed, bad_request.
//! "copy" lets selection fall back to a simulated Cmd+C, which keeps every
//! format of the clipboard; "copy_only", for tests, skips Accessibility. A token names where a selection was read; only
//! replace uses it, and only after checking the target is as it was.
//!
//! The words check, button, selection and "selection copy" alone on a line
//! still work, with id 0, for trying the helper by hand.
use std::io::{BufRead, Write};
use std::time::Instant;

mod json;

#[cfg(target_os = "macos")]
#[path = "macos.rs"]
mod platform;

#[cfg(not(target_os = "macos"))]
#[path = "unsupported.rs"]
mod platform;

use json::{quote, Value};

/// A failure the client can tell apart from others.
pub struct Fail {
    pub code: &'static str,
    pub msg: String,
}

impl Fail {
    pub fn new(code: &'static str, msg: impl Into<String>) -> Self {
        Fail { code, msg: msg.into() }
    }
}

fn fail(id: i64, f: Fail) -> String {
    format!("{{\"id\":{id},\"ok\":false,\"code\":\"{}\",\"error\":{}}}", f.code, quote(&f.msg))
}

fn handle(state: &mut platform::State, line: &str) -> String {
    let start = Instant::now();
    let (id, cmd, req) = match line.trim() {
        "check" | "button" | "selection" => (0, line.trim().to_string(), json::Object::new()),
        "selection copy" => {
            let mut o = json::Object::new();
            o.insert("copy".into(), Value::Bool(true));
            (0, "selection".into(), o)
        }
        l => match json::parse(l) {
            Ok(o) => {
                let id = match o.get("id") {
                    Some(Value::Num(n)) => *n as i64,
                    _ => 0,
                };
                let cmd = match o.get("cmd") {
                    Some(Value::Str(s)) => s.clone(),
                    _ => String::new(),
                };
                (id, cmd, o)
            }
            Err(e) => return fail(0, Fail::new("bad_request", e)),
        },
    };
    let flag = |k: &str| matches!(req.get(k), Some(Value::Bool(true)));
    let text = |k: &str| match req.get(k) {
        Some(Value::Str(s)) => Some(s.as_str()),
        _ => None,
    };
    let token = match req.get("token") {
        Some(Value::Num(n)) => *n as u64,
        _ => 0,
    };
    match cmd.as_str() {
        "check" => {
            let ok = if flag("prompt") { platform::trusted_prompt() } else { platform::trusted() };
            format!("{{\"id\":{id},\"trusted\":{ok}}}")
        }
        "button" => format!("{{\"id\":{id},\"down\":{}}}", platform::button_down()),
        "selection" => match platform::selection(state, flag("copy"), flag("copy_only")) {
            Ok(s) => format!(
                "{{\"id\":{id},\"ok\":true,\"text\":{},\"method\":\"{}\",\"us\":{},\"app\":{},\"title\":{},\"editable\":\"{}\",\"before\":{},\"after\":{},\"token\":{},\"ctx_us\":{}}}",
                quote(&s.text),
                s.method,
                start.elapsed().as_micros(),
                quote(&s.app),
                quote(&s.title),
                s.editable,
                quote(&s.before),
                quote(&s.after),
                s.token,
                s.ctx_us,
            ),
            Err(f) => fail(id, f),
        },
        "replace" => match text("text") {
            None => fail(id, Fail::new("bad_request", "replace needs text")),
            Some(t) => match platform::replace(state, token, t, text("method").unwrap_or("auto")) {
                Ok((method, verified)) => format!(
                    "{{\"id\":{id},\"ok\":true,\"method\":\"{method}\",\"verified\":{verified},\"us\":{}}}",
                    start.elapsed().as_micros()
                ),
                Err(f) => fail(id, f),
            },
        },
        "forget" => {
            platform::forget(state, token);
            format!("{{\"id\":{id},\"ok\":true}}")
        }
        other => fail(id, Fail::new("bad_request", format!("unknown request {other}"))),
    }
}

fn main() {
    let mut state = platform::State::new();
    let stdin = std::io::stdin();
    let mut stdout = std::io::stdout();
    for line in stdin.lock().lines().map_while(Result::ok) {
        if line.trim().is_empty() {
            continue;
        }
        let reply = handle(&mut state, &line);
        let _ = writeln!(stdout, "{reply}");
        let _ = stdout.flush();
    }
}
