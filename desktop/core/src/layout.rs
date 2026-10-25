#[derive(Debug, Clone, PartialEq)]
pub struct WindowBox {
    pub width: f64,
    pub height: f64,
    pub ui_scale: f64,
}

/// Extra type scale when Windows leaves a 4K panel at 100% (logical width above 1920).
/// At 150–200% DPI, logical width drops back toward 1920 and the OS scale does the rest.
pub fn ui_scale(logical_width: f64) -> f64 {
    if logical_width < 100.0 {
        return 1.0;
    }
    (logical_width / 1920.0).clamp(1.0, 2.4)
}

pub fn popup_window(logical_w: f64, logical_h: f64) -> WindowBox {
    sized(logical_w, logical_h, 1040.0, 820.0, 0.92, 0.90)
}

pub fn main_window(logical_w: f64, logical_h: f64) -> WindowBox {
    sized(logical_w, logical_h, 520.0, 860.0, 0.92, 0.92)
}

fn sized(lw: f64, lh: f64, pref_w: f64, pref_h: f64, max_wr: f64, max_hr: f64) -> WindowBox {
    if lw < 100.0 || lh < 100.0 {
        return WindowBox {
            width: pref_w,
            height: pref_h,
            ui_scale: 1.0,
        };
    }
    let ui = ui_scale(lw);
    let max_w = lw * max_wr;
    let max_h = lh * max_hr;
    WindowBox {
        width: (pref_w * ui).min(max_w).max(320.0).min(max_w),
        height: (pref_h * ui).min(max_h).max(280.0).min(max_h),
        ui_scale: ui,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn four_k_at_100_percent_is_large() {
        let popup = popup_window(3840.0, 2160.0);
        assert!((popup.ui_scale - 2.0).abs() < 0.01);
        assert!((popup.width - 2080.0).abs() < 1.0);
        assert!((popup.height - 1640.0).abs() < 1.0);
        let main = main_window(3840.0, 2160.0);
        assert!(main.width > 900.0);
        assert!(main.height > 1400.0);
    }

    #[test]
    fn full_hd_and_hidpi_logical_1080_stay_readable() {
        let popup = popup_window(1920.0, 1080.0);
        assert!((popup.ui_scale - 1.0).abs() < 0.01);
        assert!((popup.width - 1040.0).abs() < 1.0);
        assert!((popup.height - 820.0).abs() < 1.0);
    }

    #[test]
    fn scaled_4k_uses_logical_size() {
        // 3840x2160 at 150% → logical 2560x1440
        let popup = popup_window(2560.0, 1440.0);
        let expect = 2560.0 / 1920.0;
        assert!((popup.ui_scale - expect).abs() < 0.01);
        assert!((popup.width - 1040.0 * expect).abs() < 1.0);
    }

    #[test]
    fn small_laptop_stays_on_screen() {
        let popup = popup_window(1280.0, 720.0);
        assert!(popup.width <= 1280.0 * 0.92 + 0.1);
        assert!(popup.height <= 720.0 * 0.90 + 0.1);
    }
}
