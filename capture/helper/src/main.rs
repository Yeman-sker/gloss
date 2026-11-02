//! Reads the text selected in the frontmost app. One request per stdin line,
//! one JSON reply per stdout line:
//!   check           {"trusted":bool}
//!   selection       {"ok":..,"text":..,"method":..,"us":..}, platform API only
//!   selection copy  the same, with a simulated copy as the fallback
//!   button          {"down":..}, the primary mouse button anywhere on the desktop
use std::io::{BufRead, Write};
use std::time::Instant;

#[cfg(target_os = "macos")]
#[path = "macos.rs"]
mod platform;

#[cfg(not(target_os = "macos"))]
#[path = "unsupported.rs"]
mod platform;

fn json(s: &str) -> String {
    let mut out = String::from("\"");
    for ch in s.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

fn main() {
    let stdin = std::io::stdin();
    let mut stdout = std::io::stdout();
    for line in stdin.lock().lines().map_while(Result::ok) {
        let start = Instant::now();
        let reply = match line.trim() {
            "check" => format!("{{\"trusted\":{}}}", platform::trusted()),
            "button" => format!("{{\"down\":{}}}", platform::button_down()),
            req @ ("selection" | "selection copy") => {
                let (result, method) = match platform::read_selection() {
                    Ok(t) => (Ok(t), platform::API),
                    Err(_) if req == "selection copy" => (platform::copy_selection(), "copy"),
                    Err(e) => (Err(e), platform::API),
                };
                let us = start.elapsed().as_micros();
                match result {
                    Ok(t) => format!("{{\"ok\":true,\"text\":{},\"method\":\"{method}\",\"us\":{us}}}", json(&t)),
                    Err(e) => format!("{{\"ok\":false,\"error\":{},\"method\":\"{method}\",\"us\":{us}}}", json(&e)),
                }
            }
            other => format!("{{\"ok\":false,\"error\":{}}}", json(&format!("unknown request {other}"))),
        };
        let _ = writeln!(stdout, "{reply}");
        let _ = stdout.flush();
    }
}
