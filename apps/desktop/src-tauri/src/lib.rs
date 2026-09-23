// DEEIX Chat desktop shell.
//
// Scope is intentionally narrow: window/tray lifecycle, OAuth loopback receiver,
// auto-update and OS keychain storage. No business logic lives here — everything
// the user interacts with is the apps/web build, so a feature is written once.

mod commands;
mod oauth_loopback;
mod tray;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .manage(oauth_loopback::LoopbackState::default())
        .invoke_handler(tauri::generate_handler![
            commands::read_refresh_token,
            commands::write_refresh_token,
            commands::clear_refresh_token,
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
