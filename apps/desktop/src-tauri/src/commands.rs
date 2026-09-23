// Keychain commands. The refresh token is the only long-lived credential the
// desktop shell holds; it never reaches the webview's storage APIs and the
// access token stays in JS memory (see packages/core).
//
// All three commands are registered in tauri.conf.json's capability file, so the
// webview can call nothing else.

use keyring::Entry;
use serde::Serialize;

/// Service name under which entries are stored in the OS credential store.
const KEYRING_SERVICE: &str = "com.deeix.chat.desktop";

/// Account name for the single stored session.
///
/// One entry is intentional: the desktop app talks to one user-chosen server at a
/// time, and switching servers drops the previous session rather than keeping a
/// set of dormant credentials around.
const KEYRING_ACCOUNT: &str = "refresh-token";

#[derive(Debug, Serialize)]
pub struct KeychainError {
    message: String,
}

impl From<keyring::Error> for KeychainError {
    fn from(error: keyring::Error) -> Self {
        Self {
            message: error.to_string(),
        }
    }
}

impl std::fmt::Display for KeychainError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "{}", self.message)
    }
}

fn entry() -> Result<Entry, KeychainError> {
    Ok(Entry::new(KEYRING_SERVICE, KEYRING_ACCOUNT)?)
}

/// Read the stored refresh token.
///
/// A missing entry is not an error: it just means the user is signed out.
#[tauri::command]
pub fn read_refresh_token() -> Result<Option<String>, KeychainError> {
    match entry()?.get_password() {
        Ok(token) => Ok(Some(token)),
        Err(keyring::Error::NoEntry) => Ok(None),
        Err(error) => Err(error.into()),
    }
}

/// Store (or replace) the refresh token.
#[tauri::command]
pub fn write_refresh_token(token: String) -> Result<(), KeychainError> {
    if token.trim().is_empty() {
        return clear_refresh_token();
    }
    entry()?.set_password(&token)?;
    Ok(())
}

/// Remove the stored refresh token. Signing out twice is not an error.
#[tauri::command]
pub fn clear_refresh_token() -> Result<(), KeychainError> {
    match entry()?.delete_credential() {
        Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
        Err(error) => Err(error.into()),
    }
}
