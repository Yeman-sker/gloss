//! Just enough JSON for the requests: one flat object of strings, numbers,
//! booleans and nulls. The helper has no dependencies, so it reads them by
//! hand rather than pull in serde.
use std::collections::HashMap;

#[derive(Debug, PartialEq)]
pub enum Value {
    Str(String),
    Num(f64),
    Bool(bool),
    Null,
}

pub type Object = HashMap<String, Value>;

pub fn quote(s: &str) -> String {
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

pub fn parse(src: &str) -> Result<Object, String> {
    let mut p = Parser { s: src.as_bytes(), i: 0 };
    p.ws();
    p.expect(b'{')?;
    let mut out = Object::new();
    p.ws();
    if p.peek() == Some(b'}') {
        p.i += 1;
    } else {
        p.members(&mut out)?;
    }
    p.ws();
    if p.i != p.s.len() {
        return Err("trailing data".into());
    }
    Ok(out)
}

impl Parser<'_> {
    fn members(&mut self, out: &mut Object) -> Result<(), String> {
        let p = self;
        loop {
            p.ws();
            let key = p.string()?;
            p.ws();
            p.expect(b':')?;
            p.ws();
            let v = p.value()?;
            out.insert(key, v);
            p.ws();
            match p.next() {
                Some(b',') => continue,
                Some(b'}') => return Ok(()),
                _ => return Err("expected , or }".into()),
            }
        }
    }
}

struct Parser<'a> {
    s: &'a [u8],
    i: usize,
}

impl Parser<'_> {
    fn peek(&self) -> Option<u8> {
        self.s.get(self.i).copied()
    }
    fn next(&mut self) -> Option<u8> {
        let c = self.peek();
        self.i += 1;
        c
    }
    fn ws(&mut self) {
        while matches!(self.peek(), Some(b' ' | b'\t' | b'\n' | b'\r')) {
            self.i += 1;
        }
    }
    fn expect(&mut self, c: u8) -> Result<(), String> {
        if self.next() == Some(c) {
            Ok(())
        } else {
            Err(format!("expected {}", c as char))
        }
    }
    fn value(&mut self) -> Result<Value, String> {
        match self.peek() {
            Some(b'"') => Ok(Value::Str(self.string()?)),
            Some(b't') => self.word("true", Value::Bool(true)),
            Some(b'f') => self.word("false", Value::Bool(false)),
            Some(b'n') => self.word("null", Value::Null),
            Some(b'-' | b'0'..=b'9') => {
                let start = self.i;
                while matches!(self.peek(), Some(b'-' | b'+' | b'.' | b'e' | b'E' | b'0'..=b'9')) {
                    self.i += 1;
                }
                let text = std::str::from_utf8(&self.s[start..self.i]).map_err(|e| e.to_string())?;
                text.parse().map(Value::Num).map_err(|_| "bad number".into())
            }
            _ => Err("unsupported value".into()),
        }
    }
    fn word(&mut self, w: &str, v: Value) -> Result<Value, String> {
        if self.s[self.i..].starts_with(w.as_bytes()) {
            self.i += w.len();
            Ok(v)
        } else {
            Err("bad literal".into())
        }
    }
    fn hex4(&mut self) -> Result<u32, String> {
        let h = self.s.get(self.i..self.i + 4).ok_or("short escape")?;
        self.i += 4;
        u32::from_str_radix(std::str::from_utf8(h).map_err(|e| e.to_string())?, 16).map_err(|_| "bad escape".into())
    }
    fn string(&mut self) -> Result<String, String> {
        self.expect(b'"')?;
        let mut out = Vec::new();
        loop {
            match self.next().ok_or("unterminated string")? {
                b'"' => break,
                b'\\' => {
                    let c = match self.next().ok_or("bad escape")? {
                        b'"' => '"',
                        b'\\' => '\\',
                        b'/' => '/',
                        b'b' => '\u{8}',
                        b'f' => '\u{c}',
                        b'n' => '\n',
                        b'r' => '\r',
                        b't' => '\t',
                        b'u' => {
                            let mut cp = self.hex4()?;
                            if (0xD800..0xDC00).contains(&cp) && self.s[self.i..].starts_with(b"\\u") {
                                self.i += 2;
                                let lo = self.hex4()?;
                                cp = 0x10000 + ((cp - 0xD800) << 10) + (lo.wrapping_sub(0xDC00) & 0x3FF);
                            }
                            char::from_u32(cp).unwrap_or('\u{FFFD}')
                        }
                        _ => return Err("bad escape".into()),
                    };
                    let mut buf = [0u8; 4];
                    out.extend_from_slice(c.encode_utf8(&mut buf).as_bytes());
                }
                b => out.push(b),
            }
        }
        String::from_utf8(out).map_err(|e| e.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_requests() {
        let o = parse(r#" {"id": 7, "cmd":"replace", "copy":true, "text":"a\n\"b\" \u4f60\ud83d\ude00", "x":null} "#).unwrap();
        assert_eq!(o["id"], Value::Num(7.0));
        assert_eq!(o["copy"], Value::Bool(true));
        assert_eq!(o["text"], Value::Str("a\n\"b\" 你😀".into()));
        assert_eq!(o["x"], Value::Null);
        assert!(parse("{}").unwrap().is_empty());
    }

    #[test]
    fn rejects_garbage() {
        for bad in ["", "[]", "{\"a\"}", "{\"a\":1", "{\"a\":1} x", "{\"a\":[1]}", "{\"a\":\"\\q\"}"] {
            assert!(parse(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn quotes_round_trip() {
        let s = "line\nquote\" back\\ tab\t \u{1} 你";
        let o = parse(&format!("{{\"t\":{}}}", quote(s))).unwrap();
        assert_eq!(o["t"], Value::Str(s.into()));
    }
}
