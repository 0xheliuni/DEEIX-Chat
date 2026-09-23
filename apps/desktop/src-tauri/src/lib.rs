// DEEIX Chat desktop shell.
//
// Scope is intentionally narrow: window/tray lifecycle, OAuth loopback receiver,
// auto-update and session persistence (refresh token never leaves Rust after login). No business logic lives here — everything
// the user interacts with is the apps/web build, so a feature is written once.

mod oauth_loopback;
mod session;
mod tray;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // One TLS provider for both the updater and session refresh.
    session::ensure_tls_provider();

    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .manage(oauth_loopback::LoopbackState::default())
        .invoke_handler(tauri::generate_handler![
            session::get_server_origin,
            session::set_server_origin,
            session::store_session,
            session::has_session,
            session::clear_session,
            session::refresh_session,
            oauth_loopback::start_oauth_loopback,
            oauth_loopback::stop_oauth_loopback,
        ])
        .setup(|app| {
            #[cfg(desktop)]
            tray::create_tray(app.handle())?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running DEEIX Chat desktop");
}
