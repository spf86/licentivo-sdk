//! High-level application licensing backed by the same verified core as C/C++.
//! Keep one client alive while the application runs. Drop/close stops policy
//! heartbeats and releases floating seats; normal exits retain device bindings.
use serde_json::Value;
use std::{ffi::{CStr, CString}, fmt, os::raw::{c_char, c_int, c_void}, path::Path,
    panic::{catch_unwind, resume_unwind, AssertUnwindSafe}, ptr::NonNull};

#[repr(C)]
struct NativeClient { _private: [u8; 0] }
#[repr(C)]
struct NativeError { http_status: c_int, code: [c_char; 80], request_id: [c_char; 128], retryable: c_int, retry_after_seconds: c_int }
type Action = unsafe extern "C" fn(*mut c_void) -> c_int;
extern "C" {
    fn ln_init_file_with_license(path: *const c_char, key: *const c_char) -> *mut NativeClient;
    fn ln_start(client: *mut NativeClient) -> c_int;
    fn ln_destroy(client: *mut NativeClient);
    fn ln_get_error_info(client: *mut NativeClient, error: *mut NativeError) -> c_int;
    fn ln_status(client: *mut NativeClient) -> *mut c_char;
    fn ln_feature_status(client: *mut NativeClient, feature: *const c_char) -> *mut c_char;
    fn ln_run_feature(client: *mut NativeClient, feature: *const c_char, action: Action, context: *mut c_void) -> c_int;
    fn ln_run_metered_feature(client: *mut NativeClient, feature: *const c_char, quantity: i64, id: *const c_char, action: Action, context: *mut c_void) -> c_int;
    fn ln_resolve_metered_feature(client: *mut NativeClient, id: *const c_char, succeeded: c_int) -> c_int;
    fn ln_validate(client: *mut NativeClient) -> c_int;
    fn ln_heartbeat(client: *mut NativeClient) -> c_int;
    fn ln_refresh_online(client: *mut NativeClient) -> c_int;
    fn ln_deactivate(client: *mut NativeClient) -> c_int;
    fn ln_reserve(client: *mut NativeClient, feature: *const c_char, quantity: i64, id: *const c_char) -> *mut c_char;
    fn ln_commit(client: *mut NativeClient, reservation: *const c_char, id: *const c_char) -> *mut c_char;
    fn ln_cancel(client: *mut NativeClient, reservation: *const c_char, id: *const c_char) -> *mut c_char;
    fn ln_consume(client: *mut NativeClient, feature: *const c_char, quantity: i64, id: *const c_char) -> *mut c_char;
    fn ln_offline_request(client: *mut NativeClient, operation: *const c_char) -> *mut c_char;
    fn ln_import_offline(client: *mut NativeClient, request: *const c_char, response: *const c_char) -> c_int;
    fn ln_free_string(value: *mut c_char);
    fn ln_json(value: *const c_char) -> *mut c_void;
    fn cJSON_Delete(value: *mut c_void);
    fn ln_verify(envelope: *const c_void, pins: *const c_void, product: *const c_char, device: *const c_char, now: i64) -> c_int;
}

#[derive(Debug)]
pub struct Error {
    pub message: String,
    pub code: String,
    pub http_status: i32,
    pub request_id: String,
    pub retryable: bool,
    pub retry_after_seconds: i32,
}
impl Error {
    pub fn business(message: impl Into<String>) -> Self { Self::local("BUSINESS_FAILED", message) }
    fn local(code: &str, message: impl Into<String>) -> Self {
        Self { message: message.into(), code: code.into(), http_status: 0, request_id: String::new(), retryable: false, retry_after_seconds: 0 }
    }
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.code, self.message)?;
        if !self.request_id.is_empty() { write!(f, " (request {})", self.request_id)?; }
        Ok(())
    }
}
impl std::error::Error for Error {}
impl From<std::io::Error> for Error { fn from(error: std::io::Error) -> Self { Self::business(error.to_string()) } }

/// A confirmed retry may skip the business callback. Do not assume a replay
/// recreates the business result; load the existing result using your job ID.
#[derive(Debug, PartialEq)]
pub enum Execution<T> { Executed(T), AlreadyCompleted }

pub struct Client { native: NonNull<NativeClient> }
// The core owns its state, locks native access and joins its heartbeat worker on
// destruction. No Rust callback is retained after a synchronous method returns.
// Ownership can move to a worker thread; concurrent shared access is not exposed.
unsafe impl Send for Client {}
impl Client {
    pub fn open_with_license(path: impl AsRef<Path>, customer_key: &str) -> Result<Self, Error> {
        let path = path.as_ref().to_str().ok_or_else(|| Error::local("INVALID_INPUT", "Product path must be valid UTF-8"))?;
        let (path, key) = (input(path)?, input(customer_key)?);
        let native = NonNull::new(unsafe { ln_init_file_with_license(path.as_ptr(), key.as_ptr()) })
            .ok_or_else(|| Error::local("CONFIGURATION_ERROR", "Check product.json, pinned keys and local storage"))?;
        let client = Self { native };
        client.checked(unsafe { ln_start(client.ptr()) })?;
        Ok(client)
    }
    fn ptr(&self) -> *mut NativeClient { self.native.as_ptr() }
    fn error(&self) -> Error {
        let mut native = NativeError { http_status: 0, code: [0;80], request_id: [0;128], retryable: 0, retry_after_seconds: 0 };
        unsafe { ln_get_error_info(self.ptr(), &mut native); }
        Error { message: "The licensing operation did not complete".into(),
            code: unsafe { text(native.code.as_ptr()) }, http_status: native.http_status,
            request_id: unsafe { text(native.request_id.as_ptr()) }, retryable: native.retryable != 0, retry_after_seconds: native.retry_after_seconds }
    }
    fn checked(&self, ok: c_int) -> Result<(), Error> { if ok != 0 { Ok(()) } else { Err(self.error()) } }
    fn json(&self, raw: *mut c_char) -> Result<Value, Error> {
        if raw.is_null() { return Err(self.error()); }
        let result = serde_json::from_slice(unsafe { CStr::from_ptr(raw).to_bytes() })
            .map_err(|_| Error::local("INVALID_RESPONSE", "Native core returned an invalid JSON result"));
        unsafe { ln_free_string(raw); }
        result
    }
    pub fn status(&self) -> Result<Value, Error> { self.json(unsafe { ln_status(self.ptr()) }) }
    pub fn feature_status(&self, feature: &str) -> Result<Value, Error> {
        let feature = input(feature)?; self.json(unsafe { ln_feature_status(self.ptr(), feature.as_ptr()) })
    }
    pub fn run_feature<T,F: FnOnce() -> Result<T, Error>>(&mut self, feature: &str, action: F) -> Result<T, Error> {
        let feature = input(feature)?;
        let mut callback = Callback::new(action);
        let ok = unsafe { ln_run_feature(self.ptr(), feature.as_ptr(), bridge::<F,T>, callback.context()) };
        callback.finish(ok, self)?.ok_or_else(|| Error::local("BUSINESS_FAILED", "The business callback did not run"))
    }
    pub fn run_metered_feature<T,F: FnOnce() -> Result<T, Error>>(&mut self, feature: &str, quantity: i64, job_id: &str, action: F) -> Result<Execution<T>, Error> {
        let (feature, id) = (input(feature)?, input(job_id)?);
        let mut callback = Callback::new(action);
        let ok = unsafe { ln_run_metered_feature(self.ptr(), feature.as_ptr(), quantity, id.as_ptr(), bridge::<F,T>, callback.context()) };
        Ok(match callback.finish(ok, self)? { Some(value) => Execution::Executed(value), None => Execution::AlreadyCompleted })
    }
    /// Settle an interrupted callback only after checking the real business
    /// result. This method never executes the business callback.
    pub fn resolve_metered_feature(&mut self, job_id: &str, business_succeeded: bool) -> Result<(), Error> {
        let id = input(job_id)?; self.checked(unsafe { ln_resolve_metered_feature(self.ptr(), id.as_ptr(), business_succeeded as c_int) })
    }
    pub fn validate(&mut self) -> Result<(), Error> { self.checked(unsafe { ln_validate(self.ptr()) }) }
    pub fn heartbeat(&mut self) -> Result<(), Error> { self.checked(unsafe { ln_heartbeat(self.ptr()) }) }
    pub fn refresh_online(&mut self) -> Result<(), Error> { self.checked(unsafe { ln_refresh_online(self.ptr()) }) }
    pub fn deactivate(&mut self) -> Result<(), Error> { self.checked(unsafe { ln_deactivate(self.ptr()) }) }
    pub fn close(self) { drop(self); }
    pub fn reserve(&mut self, feature: &str, quantity: i64, job_id: &str) -> Result<Value, Error> {
        let (feature, id) = (input(feature)?, input(job_id)?);
        self.json(unsafe { ln_reserve(self.ptr(), feature.as_ptr(), quantity, id.as_ptr()) })
    }
    pub fn commit(&mut self, reservation_id: &str, job_id: &str) -> Result<Value, Error> {
        let (reservation, id) = (input(reservation_id)?, input(job_id)?);
        self.json(unsafe { ln_commit(self.ptr(), reservation.as_ptr(), id.as_ptr()) })
    }
    pub fn cancel(&mut self, reservation_id: &str, job_id: &str) -> Result<Value, Error> {
        let (reservation, id) = (input(reservation_id)?, input(job_id)?);
        self.json(unsafe { ln_cancel(self.ptr(), reservation.as_ptr(), id.as_ptr()) })
    }
    pub fn consume(&mut self, feature: &str, quantity: i64, job_id: &str) -> Result<Value, Error> {
        let (feature, id) = (input(feature)?, input(job_id)?);
        self.json(unsafe { ln_consume(self.ptr(), feature.as_ptr(), quantity, id.as_ptr()) })
    }
    pub fn offline_request(&mut self, operation: &str) -> Result<Value, Error> {
        let operation = input(operation)?; self.json(unsafe { ln_offline_request(self.ptr(), operation.as_ptr()) })
    }
    pub fn import_offline(&mut self, request: &Value, response: &Value) -> Result<(), Error> {
        let (request, response) = (input(&request.to_string())?, input(&response.to_string())?);
        self.checked(unsafe { ln_import_offline(self.ptr(), request.as_ptr(), response.as_ptr()) })
    }
}
impl Drop for Client { fn drop(&mut self) { unsafe { ln_destroy(self.ptr()); } } }

struct Callback<F,T> { action: Option<F>, result: Option<Result<T,Error>>, panic: Option<Box<dyn std::any::Any + Send>> }
impl<F: FnOnce() -> Result<T,Error>,T> Callback<F,T> {
    fn new(action: F) -> Self { Self { action: Some(action), result: None, panic: None } }
    fn context(&mut self) -> *mut c_void { self as *mut Self as *mut c_void }
    fn finish(mut self, ok: c_int, client: &Client) -> Result<Option<T>,Error> {
        if let Some(panic) = self.panic.take() { resume_unwind(panic); }
        if matches!(self.result, Some(Err(_))) {
            return self.result.take().unwrap().map(Some);
        }
        client.checked(ok)?;
        self.result.take().transpose()
    }
}
unsafe extern "C" fn bridge<F: FnOnce() -> Result<T,Error>,T>(context: *mut c_void) -> c_int {
    let callback = &mut *(context as *mut Callback<F,T>);
    let Some(action) = callback.action.take() else { return 0; };
    // Rust must never unwind across the C ABI. The core cancels a failed
    // callback; resume the original panic only after returning into Rust.
    match catch_unwind(AssertUnwindSafe(action)) {
        Ok(result) => { let ok = result.is_ok(); callback.result = Some(result); ok as c_int }
        Err(panic) => { callback.panic = Some(panic); 0 }
    }
}
fn input(value: &str) -> Result<CString,Error> { CString::new(value).map_err(|_| Error::local("INVALID_INPUT", "Input must not contain NUL bytes")) }
unsafe fn text(value: *const c_char) -> String { if value.is_null() { String::new() } else { CStr::from_ptr(value).to_string_lossy().into_owned() } }

/// Verification helper for signed lease fixtures. Applications normally use
/// Client, which also enforces the runtime policy and manages credential state.
pub fn verify_lease(envelope: &Value, pins: &Value, product: &str, device: &str, now: i64) -> Result<bool,Error> {
    let (envelope_text, pins_text, product, device) = (input(&envelope.to_string())?, input(&pins.to_string())?, input(product)?, input(device)?);
    let (envelope, pins) = unsafe { (ln_json(envelope_text.as_ptr()), ln_json(pins_text.as_ptr())) };
    let ok = unsafe { ln_verify(envelope, pins, product.as_ptr(), device.as_ptr(), now) != 0 };
    unsafe { cJSON_Delete(envelope); cJSON_Delete(pins); }
    Ok(ok)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test] fn ffi_error_layout() { assert_eq!(std::mem::size_of::<NativeError>(), 220); }
    #[test] fn rejects_nul_before_native_call() { assert_eq!(input("export\0other").unwrap_err().code, "INVALID_INPUT"); }
    #[test] fn client_can_move_to_a_worker() { fn assert_send<T: Send>() {} assert_send::<Client>(); }
}
