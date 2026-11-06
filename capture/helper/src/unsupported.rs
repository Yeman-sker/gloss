//! Platforms without a helper yet: every request fails, and the app says so.
use crate::Fail;

pub struct State;

impl State {
    pub fn new() -> State {
        State
    }
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

pub fn trusted() -> bool {
    false
}

pub fn button_down() -> bool {
    false
}

pub fn selection(_: &mut State, _copy: bool, _copy_only: bool) -> Result<Selection, Fail> {
    Err(Fail::new("failed", "not supported on this platform yet"))
}

pub fn replace(_: &mut State, _token: u64, _text: &str, _method: &str) -> Result<(&'static str, bool), Fail> {
    Err(Fail::new("failed", "not supported on this platform yet"))
}

pub fn forget(_: &mut State, _token: u64) {}
