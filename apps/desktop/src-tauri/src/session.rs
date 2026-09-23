// Session persistence and refresh, kept on the Rust side on purpose.
//
// The refresh token is the only long-lived credential. It is stored in the OS
// keychain under an account keyed by the server origin it was issued for, and
// is only ever sent back to that origin — by this module, never by the webview.
// JavaScript hands the token over once, right after a successful login, and
// from then on can only ask for a new access token or a sign-out. This restores
// the property the browser build gets from an HttpOnly cookie: script running
// in the page cannot read or exfiltrate the long-lived credential, and cannot
// redirect it to a different server by changing a stored address.

use std::fs;
use std::path::PathBuf;
use std::time::Duration;

use keyring::Entry;
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, Runtime};

const KEYRING_SERVICE: &str = "com.deeix.chat.desktop";
const ORIGIN_FILE: &str = "server.json";
const CLIENT_PLATFORM_HEADER: &str = "X-Client-Platform";
const REFRESH_PATH: &str = "/api/v1/auth/refresh";
const HTTP_TIMEOUT: Duration = Duration::from_secs(15);
const MAX_BODY_BYTES: u64 = 256 * 1024;

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionError {
    /// "network" | "http" | "storage" | "no_session" | "invalid_origin"
    pub kind: &'static str,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error_code: Option<String>,
}

impl SessionError {
    fn storage(e: impl std::fmt::Display) -> Self {
        Self { kind: "storage", message: e.to_string(), status: None, error_code: None }
    }
    fn network(e: impl std::fmt::Display) -> Self {
        Self { kind: "network", message: e.to_string(), status: None, error_code: None }
    }
    fn no_session() -> Self {
        Self { kind: "no_session", message: "no stored session".into(), status: None, error_code: None }
    }
    fn invalid_origin(message: &str) -> Self {
        Self { kind: "invalid_origin", message: message.into(), status: None, error_code: None }
    }
}

#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionCredentials {
    pub access_token: String,
    #[serde(rename = "sessionID")]
    pub session_id: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Envelope {
    #[serde(default)]
    error_msg: String,
    #[serde(default)]
    error_code: Option<String>,
    #[serde(default)]
    data: Option<RefreshData>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct RefreshData {
    #[serde(default)]
    access_token: String,
    #[serde(rename = "sessionID", default)]
    session_id: String,
    #[serde(default)]
    refresh_token: Option<String>,
}

#[derive(Serialize, Deserialize, Default)]
struct OriginFile {
    origin: String,
}

/// reqwest is built with `rustls-no-provider`, so a process-wide crypto provider
/// must exist before the first client is constructed. Idempotent.
pub(crate) fn ensure_tls_provider() {
    if rustls::crypto::CryptoProvider::get_default().is_none() {
        let _ = rustls::crypto::ring::default_provider().install_default();
    }
}

// ---------- origin ----------

fn origin_path<R: Runtime>(app: &AppHandle<R>) -> Result<PathBuf, SessionError> {
    let dir = app.path().app_config_dir().map_err(SessionError::storage)?;
    fs::create_dir_all(&dir).map_err(SessionError::storage)?;
    Ok(dir.join(ORIGIN_FILE))
}

fn read_origin<R: Runtime>(app: &AppHandle<R>) -> Result<Option<String>, SessionError> {
    let path = origin_path(app)?;
    match fs::read(&path) {
        Ok(bytes) => {
            let file: OriginFile = serde_json::from_slice(&bytes).map_err(SessionError::storage)?;
            Ok(normalize_origin(&file.origin))
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(SessionError::storage(e)),
    }
}

/// Accept only an absolute http(s) origin with no path, query, fragment or userinfo.
pub(crate) fn normalize_origin(raw: &str) -> Option<String> {
    let url = reqwest::Url::parse(raw.trim()).ok()?;
    if !matches!(url.scheme(), "http" | "https") {
        return None;
    }
    if !url.username().is_empty() || url.password().is_some() || url.query().is_some() || url.fragment().is_some() {
        return None;
    }
    if !matches!(url.path(), "" | "/") {
        return None;
    }
    url.host_str()?;
    Some(url.origin().ascii_serialization())
}

// ---------- keychain ----------

fn entry(origin: &str) -> Result<Entry, SessionError> {
    Entry::new(KEYRING_SERVICE, &format!("refresh-token:{origin}")).map_err(SessionError::storage)
}

fn read_token(origin: &str) -> Result<Option<String>, SessionError> {
    match entry(origin)?.get_password() {
        Ok(token) => Ok(Some(token)),
        Err(keyring::Error::NoEntry) => Ok(None),
        Err(e) => Err(SessionError::storage(e)),
    }
}

fn write_token(origin: &str, token: &str) -> Result<(), SessionError> {
    entry(origin)?.set_password(token).map_err(SessionError::storage)
}

fn delete_token(origin: &str) -> Result<(), SessionError> {
    match entry(origin)?.delete_credential() {
        Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
        Err(e) => Err(SessionError::storage(e)),
    }
}

// ---------- commands ----------

/// The pinned server origin, or null on first run.
#[tauri::command]
pub fn get_server_origin<R: Runtime>(app: AppHandle<R>) -> Result<Option<String>, SessionError> {
    read_origin(&app)
}

/// Pin the server origin. Changing it drops the session for the previous
/// origin so a token can never be replayed against a different operator.
#[tauri::command]
pub fn set_server_origin<R: Runtime>(app: AppHandle<R>, origin: String) -> Result<String, SessionError> {
    let normalized = normalize_origin(&origin)
        .ok_or_else(|| SessionError::invalid_origin("origin must be an absolute http(s) URL without a path"))?;
    if let Some(previous) = read_origin(&app)? {
        if previous != normalized {
            delete_token(&previous)?;
        }
    }
    let file = OriginFile { origin: normalized.clone() };
    fs::write(origin_path(&app)?, serde_json::to_vec(&file).map_err(SessionError::storage)?)
        .map_err(SessionError::storage)?;
    Ok(normalized)
}

/// Persist the refresh token issued at login for the pinned origin.
/// This is the only moment the webview holds the token.
#[tauri::command]
pub fn store_session<R: Runtime>(app: AppHandle<R>, refresh_token: String) -> Result<(), SessionError> {
    let origin = read_origin(&app)?.ok_or_else(|| SessionError::invalid_origin("no server configured"))?;
    if refresh_token.trim().is_empty() {
        return delete_token(&origin);
    }
    write_token(&origin, refresh_token.trim())
}

/// Whether a refresh token exists for the pinned origin (never returns it).
#[tauri::command]
pub fn has_session<R: Runtime>(app: AppHandle<R>) -> Result<bool, SessionError> {
    match read_origin(&app)? {
        Some(origin) => Ok(read_token(&origin)?.is_some()),
        None => Ok(false),
    }
}

/// Drop the stored refresh token (sign-out).
#[tauri::command]
pub fn clear_session<R: Runtime>(app: AppHandle<R>) -> Result<(), SessionError> {
    match read_origin(&app)? {
        Some(origin) => delete_token(&origin),
        None => Ok(()),
    }
}

/// Exchange the stored refresh token for a new access token against the
/// pinned origin. Rotates the stored token on success; clears it when the
/// server says the session is gone. Returns only short-lived credentials.
#[tauri::command]
pub async fn refresh_session<R: Runtime>(app: AppHandle<R>) -> Result<SessionCredentials, SessionError> {
    let origin = read_origin(&app)?.ok_or_else(|| SessionError::invalid_origin("no server configured"))?;
    let token = read_token(&origin)?.ok_or_else(SessionError::no_session)?;

    match perform_refresh(&origin, &token).await {
        Ok(Refreshed { credentials, rotated_token }) => {
            if let Some(rotated) = rotated_token {
                write_token(&origin, &rotated)?;
            }
            Ok(credentials)
        }
        Err(e) => {
            // 401 for any reason means the server will not honour this token again.
            if e.status == Some(401) || e.kind == "no_session" {
                delete_token(&origin)?;
            }
            Err(e)
        }
    }
}

#[derive(Debug)]
pub(crate) struct Refreshed {
    pub credentials: SessionCredentials,
    pub rotated_token: Option<String>,
}

/// The HTTP half of a refresh, independent of any storage: POST the token to
/// `<origin>/api/v1/auth/refresh` as a native client and parse the envelope.
pub(crate) async fn perform_refresh(origin: &str, token: &str) -> Result<Refreshed, SessionError> {
    ensure_tls_provider();
    let client = reqwest::Client::builder()
        .timeout(HTTP_TIMEOUT)
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .map_err(SessionError::network)?;

    let response = client
        .post(format!("{origin}{REFRESH_PATH}"))
        .header(CLIENT_PLATFORM_HEADER, "desktop")
        .json(&serde_json::json!({ "refreshToken": token }))
        .send()
        .await
        .map_err(SessionError::network)?;

    let status = response.status().as_u16();
    let body = response.bytes().await.map_err(SessionError::network)?;
    if body.len() as u64 > MAX_BODY_BYTES {
        return Err(SessionError::network("response too large"));
    }
    let envelope: Envelope = serde_json::from_slice(&body).unwrap_or(Envelope {
        error_msg: String::from_utf8_lossy(&body).into_owned(),
        error_code: None,
        data: None,
    });

    let Some(data) = envelope.data.filter(|_| (200..300).contains(&status)) else {
        return Err(SessionError {
            kind: "http",
            message: if envelope.error_msg.is_empty() { format!("refresh failed: {status}") } else { envelope.error_msg },
            status: Some(status),
            error_code: envelope.error_code,
        });
    };
    if data.access_token.is_empty() {
        return Err(SessionError::no_session());
    }
    Ok(Refreshed {
        credentials: SessionCredentials { access_token: data.access_token, session_id: data.session_id },
        rotated_token: data.refresh_token.filter(|t| !t.is_empty()),
    })
}

#[cfg(test)]
mod tests {
    use super::normalize_origin;

    #[test]
    fn accepts_plain_origins() {
        assert_eq!(normalize_origin("https://chat.example.com").as_deref(), Some("https://chat.example.com"));
        assert_eq!(normalize_origin(" http://127.0.0.1:8080/ ").as_deref(), Some("http://127.0.0.1:8080"));
        assert_eq!(normalize_origin("HTTPS://Chat.Example.com").as_deref(), Some("https://chat.example.com"));
    }

    #[test]
    fn rejects_anything_that_is_not_an_origin() {
        for bad in [
            "chat.example.com",
            "ftp://chat.example.com",
            "https://user:pw@chat.example.com",
            "https://chat.example.com/api",
            "https://chat.example.com/?x=1",
            "https://chat.example.com/#f",
            "javascript:alert(1)",
            "",
        ] {
            assert!(normalize_origin(bad).is_none(), "{bad} should be rejected");
        }
    }
}

// Live integration test: needs a running server and a desktop-issued refresh token.
//   DEEIX_TEST_ORIGIN=http://127.0.0.1:8080 DEEIX_TEST_REFRESH_TOKEN=... cargo test --lib -- --ignored
#[cfg(test)]
mod live {
    use super::perform_refresh;

    #[tokio::test]
    #[ignore = "requires DEEIX_TEST_ORIGIN and DEEIX_TEST_REFRESH_TOKEN"]
    async fn refresh_against_live_server_rotates_token() {
        let origin = std::env::var("DEEIX_TEST_ORIGIN").expect("DEEIX_TEST_ORIGIN");
        let token = std::env::var("DEEIX_TEST_REFRESH_TOKEN").expect("DEEIX_TEST_REFRESH_TOKEN");

        let first = perform_refresh(&origin, &token).await.expect("first refresh succeeds");
        assert!(!first.credentials.access_token.is_empty());
        assert!(!first.credentials.session_id.is_empty());
        let rotated = first.rotated_token.expect("native refresh returns a rotated token in the body");
        assert_ne!(rotated, token, "server must rotate the refresh token");

        // The server keeps the previous token valid for a short grace window
        // (refreshTokenPreviousHashGrace, 15s) so a lost rotation response does
        // not strand the client. Reuse *outside* the window revokes the session;
        // that path is covered by the backend's own tests.
        let replay = perform_refresh(&origin, &token).await.expect("replay inside the grace window is tolerated");
        assert!(replay.rotated_token.is_some());

        // The rotated token is the live one.
        let second = perform_refresh(&origin, &rotated).await.expect("rotated token is valid");
        assert_ne!(second.rotated_token.as_deref(), Some(rotated.as_str()));

        // A token that was never issued is rejected with the terminating code.
        let bogus = perform_refresh(&origin, "not-a-token").await.expect_err("bogus token must fail");
        assert_eq!(bogus.status, Some(401));
        assert_eq!(bogus.error_code.as_deref(), Some("auth.invalid_refresh_token"));
    }
}
