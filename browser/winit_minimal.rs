use std::cell::RefCell;
use std::error::Error;
use std::rc::Rc;

use euclid::Scale;
use servo::{
    InputEvent, Modifiers, RenderingContext, Servo, ServoBuilder, WebView, WebViewBuilder,
    WheelDelta, WheelEvent, WheelMode, WindowRenderingContext,
};
use url::Url;
use webrender_api::units::DevicePoint;
use winit::application::ApplicationHandler;
use winit::event::{MouseScrollDelta, WindowEvent};
use winit::event_loop::EventLoop;
use winit::raw_window_handle::{HasDisplayHandle, HasWindowHandle};
use winit::window::Window;

fn main() -> Result<(), Box<dyn Error>> {
    rustls::crypto::aws_lc_rs::default_provider()
        .install_default()
        .expect("Failed to install crypto provider");

    let event_loop = EventLoop::with_user_event().build()?;
    let mut app = App::new(&event_loop);
    event_loop.run_app(&mut app)?;
    Ok(())
}

struct AppState {
    window: Window,
    servo: Servo,
    rendering_context: Rc<WindowRenderingContext>,
    webviews: RefCell<Vec<WebView>>,
}

impl servo::WebViewDelegate for AppState {
    fn notify_new_frame_ready(&self, _: WebView) {
        self.window.request_redraw();
    }
}

enum App {
    Initial(Waker),
    Running(Rc<AppState>),
}

impl App {
    fn new(event_loop: &EventLoop<WakerEvent>) -> Self {
        Self::Initial(Waker::new(event_loop))
    }
}

impl ApplicationHandler<WakerEvent> for App {
    fn resumed(&mut self, event_loop: &winit::event_loop::ActiveEventLoop) {
        if let Self::Initial(waker) = self {
            let window = event_loop
                .create_window(Window::default_attributes())
                .expect("Failed to create winit Window");
            let display_handle = event_loop.display_handle().unwrap();
            let window_handle = window.window_handle().unwrap();

            let rendering_context = Rc::new(
                WindowRenderingContext::new(display_handle, window_handle, window.inner_size())
                    .expect("Could not create RenderingContext"),
            );
            rendering_context.make_current().unwrap();

            let servo = ServoBuilder::default()
                .event_loop_waker(Box::new(waker.clone()))
                .build();

            let app_state = Rc::new(AppState {
                window,
                servo,
                rendering_context,
                webviews: Default::default(),
            });

            let url = Url::parse("https://demo.servo.org/experiments/twgl-tunnel/").unwrap();

            let webview =
                WebViewBuilder::new(&app_state.servo, app_state.rendering_context.clone())
                    .url(url)
                    .hidpi_scale_factor(Scale::new(app_state.window.scale_factor() as f32))
                    .delegate(app_state.clone())
                    .build();

            app_state.webviews.borrow_mut().push(webview);
            *self = Self::Running(app_state);
        }
    }

    fn user_event(&mut self, _: &winit::event_loop::ActiveEventLoop, _: WakerEvent) {
        if let Self::Running(state) = self {
            state.servo.spin_event_loop();
        }
    }

    fn window_event(
        &mut self,
        event_loop: &winit::event_loop::ActiveEventLoop,
        _: winit::window::WindowId,
        event: WindowEvent,
    ) {
        if let Self::Running(state) = self {
            state.servo.spin_event_loop();
        }

        match event {
            WindowEvent::CloseRequested => event_loop.exit(),
            WindowEvent::RedrawRequested => {
                if let Self::Running(state) = self {
                    let webviews = state.webviews.borrow();
                    if let Some(webview) = webviews.last() {
                        webview.paint();
                        state.rendering_context.present();
                    }
                }
            }
            WindowEvent::Resized(new_size) => {
                if let Self::Running(state) = self {
                    if let Some(webview) = state.webviews.borrow().last() {
                        webview.resize(new_size);
                    }
                }
            }
            WindowEvent::MouseWheel { delta, .. } => {
                if let Self::Running(state) = self {
                    if let Some(webview) = state.webviews.borrow().last() {
                        let (dx, dy, mode) = match delta {
                            MouseScrollDelta::LineDelta(dx, dy) => {
                                ((dx * 76.0) as f64, (dy * 76.0) as f64, WheelMode::DeltaLine)
                            }
                            MouseScrollDelta::PixelDelta(d) => (d.x, d.y, WheelMode::DeltaPixel),
                        };
                        webview.notify_input_event(InputEvent::Wheel(WheelEvent::new(
                            WheelDelta { x: dx, y: dy, z: 0.0, mode },
                            DevicePoint::default().into(),
                            Modifiers::default(),
                        )));
                    }
                }
            }
            _ => (),
        }
    }
}

#[derive(Clone)]
struct Waker(winit::event_loop::EventLoopProxy<WakerEvent>);
#[derive(Debug)]
struct WakerEvent;

impl Waker {
    fn new(event_loop: &EventLoop<WakerEvent>) -> Self {
        Self(event_loop.create_proxy())
    }
}

impl servo::EventLoopWaker for Waker {
    fn clone_box(&self) -> Box<dyn servo::EventLoopWaker> {
        Box::new(self.clone())
    }
    fn wake(&self) {
        let _ = self.0.send_event(WakerEvent);
    }
}