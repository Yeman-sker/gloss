//! Platforms without an implementation yet. Windows goes here next.
pub const API: &str = "none";

pub fn button_down() -> bool {
    false
}

pub fn trusted() -> bool {
    false
}

pub fn read_selection() -> Result<String, String> {
    Err("selection is not implemented on this platform".into())
}

pub fn copy_selection() -> Result<String, String> {
    read_selection()
}
