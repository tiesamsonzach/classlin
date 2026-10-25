fn main() {
    let attributes = tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&[
            "load_config",
            "save_config",
            "latest_message",
            "list_history",
            "open_message",
            "dismiss_popup",
            "connection_status",
            "screen_metrics",
            "generate_classroom_id",
        ]),
    );
    if let Err(error) = tauri_build::try_build(attributes) {
        panic!("failed to run tauri-build: {error:#}");
    }
}
