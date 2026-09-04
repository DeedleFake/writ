//! Writ WASM guest example (greet / unless / version), matching `example/wasmhello`.
//!
//! Build:
//!
//! ```text
//! cargo build --target wasm32-wasip1 --release
//! ```
//!
//! The `.wasm` is at `target/wasm32-wasip1/release/wasmhello_rust.wasm`.

use std::sync::Mutex;

const TAG_INT: u8 = 1;
const TAG_STRING: u8 = 3;
const TAG_SYMBOL: u8 = 4;
const TAG_LIST: u8 = 5;
const TAG_ERROR: u8 = 0xff;
const INT_I64: u8 = 0;

const PKG_VAL: u8 = 0;
const PKG_FUNC: u8 = 1;
const PKG_MACRO: u8 = 2;

const CALL_FUNC: i32 = 0;
const CALL_MACRO: i32 = 1;

static ALLOCS: Mutex<Vec<Vec<u8>>> = Mutex::new(Vec::new());
static RET: Mutex<Vec<u8>> = Mutex::new(Vec::new());

#[derive(Clone)]
enum Form {
    Int(i64),
    String(String),
    Symbol(String),
    Call(Vec<Form>),
    Vec(Vec<Form>),
}

#[derive(Clone)]
enum Val {
    Int(i64),
    String(String),
    Symbol(String),
    Call(Vec<Val>),
    Vec(Vec<Val>),
}

struct R<'a> {
    b: &'a [u8],
    i: usize,
}

impl<'a> R<'a> {
    fn new(b: &'a [u8]) -> Self {
        Self { b, i: 0 }
    }
    fn u8(&mut self) -> Result<u8, String> {
        if self.i >= self.b.len() {
            return Err("eof".into());
        }
        let v = self.b[self.i];
        self.i += 1;
        Ok(v)
    }
    fn fill(&mut self, n: usize) -> Result<&'a [u8], String> {
        if self.i + n > self.b.len() {
            return Err("eof".into());
        }
        let s = &self.b[self.i..self.i + n];
        self.i += n;
        Ok(s)
    }
    fn u32(&mut self) -> Result<u32, String> {
        let s = self.fill(4)?;
        Ok(u32::from_le_bytes(s.try_into().unwrap()))
    }
    fn u64(&mut self) -> Result<u64, String> {
        let s = self.fill(8)?;
        Ok(u64::from_le_bytes(s.try_into().unwrap()))
    }
    fn i64(&mut self) -> Result<i64, String> {
        Ok(self.u64()? as i64)
    }
    fn str(&mut self) -> Result<String, String> {
        let n = self.u32()? as usize;
        let s = self.fill(n)?;
        String::from_utf8(s.to_vec()).map_err(|_| "utf8".into())
    }
}

struct W {
    b: Vec<u8>,
}

impl W {
    fn new() -> Self {
        Self { b: Vec::new() }
    }
    fn u8(&mut self, v: u8) {
        self.b.push(v);
    }
    fn u32(&mut self, v: u32) {
        self.b.extend_from_slice(&v.to_le_bytes());
    }
    fn u64(&mut self, v: u64) {
        self.b.extend_from_slice(&v.to_le_bytes());
    }
    fn i64(&mut self, v: i64) {
        self.u64(v as u64);
    }
    fn str(&mut self, s: &str) {
        self.u32(s.len() as u32);
        self.b.extend_from_slice(s.as_bytes());
    }
    fn enc_val(&mut self, v: &Val) -> Result<(), String> {
        match v {
            Val::Int(n) => {
                self.u8(TAG_INT);
                self.u8(INT_I64);
                self.i64(*n);
            }
            Val::String(s) => {
                self.u8(TAG_STRING);
                self.str(s);
            }
            Val::Symbol(s) => {
                self.u8(TAG_SYMBOL);
                self.str(s);
            }
            Val::Call(xs) | Val::Vec(xs) => {
                self.u8(TAG_LIST);
                self.u8(matches!(v, Val::Vec(_)) as u8);
                self.u32(xs.len() as u32);
                for x in xs {
                    self.enc_val(x)?;
                }
            }
        }
        Ok(())
    }
    fn enc_form(&mut self, v: &Form) -> Result<(), String> {
        match v {
            Form::Int(n) => {
                self.u8(TAG_INT);
                self.u8(INT_I64);
                self.i64(*n);
            }
            Form::String(s) => {
                self.u8(TAG_STRING);
                self.str(s);
            }
            Form::Symbol(s) => {
                self.u8(TAG_SYMBOL);
                self.str(s);
            }
            Form::Call(xs) | Form::Vec(xs) => {
                self.u8(TAG_LIST);
                self.u8(matches!(v, Form::Vec(_)) as u8);
                self.u32(xs.len() as u32);
                for x in xs {
                    self.enc_form(x)?;
                }
            }
        }
        Ok(())
    }
}

fn dec_val(r: &mut R<'_>) -> Result<Val, String> {
    match r.u8()? {
        TAG_INT => {
            let mode = r.u8()?;
            if mode != INT_I64 {
                return Err("int mode".into());
            }
            Ok(Val::Int(r.i64()?))
        }
        TAG_STRING => Ok(Val::String(r.str()?)),
        TAG_SYMBOL => Ok(Val::Symbol(r.str()?)),
        TAG_LIST => {
            let vec = r.u8()?;
            let n = r.u32()? as usize;
            let mut xs = Vec::with_capacity(n);
            for _ in range(n) {
                xs.push(dec_val(r)?);
            }
            if vec == 1 {
                Ok(Val::Vec(xs))
            } else {
                Ok(Val::Call(xs))
            }
        }
        t => Err(format!("val tag {t}")),
    }
}

fn dec_form(r: &mut R<'_>) -> Result<Form, String> {
    match r.u8()? {
        TAG_INT => {
            let mode = r.u8()?;
            if mode != INT_I64 {
                return Err("int mode".into());
            }
            Ok(Form::Int(r.i64()?))
        }
        TAG_STRING => Ok(Form::String(r.str()?)),
        TAG_SYMBOL => Ok(Form::Symbol(r.str()?)),
        TAG_LIST => {
            let vec = r.u8()?;
            let n = r.u32()? as usize;
            let mut xs = Vec::with_capacity(n);
            for _ in range(n) {
                xs.push(dec_form(r)?);
            }
            if vec == 1 {
                Ok(Form::Vec(xs))
            } else {
                Ok(Form::Call(xs))
            }
        }
        t => Err(format!("form tag {t}")),
    }
}

fn range(n: usize) -> impl Iterator<Item = usize> {
    0..n
}

fn enc_err(msg: &str) -> Vec<u8> {
    let mut w = W::new();
    w.u8(TAG_ERROR);
    w.str(msg);
    w.b
}

fn package_table() -> Vec<u8> {
    let mut w = W::new();
    // vals, funcs, macros — sorted names within each kind
    w.u32(3);
    w.u8(PKG_VAL);
    w.str("version");
    w.enc_val(&Val::Int(1)).unwrap();
    w.u8(PKG_FUNC);
    w.str("greet");
    w.u8(PKG_MACRO);
    w.str("unless");
    w.b
}

fn greet(args: &[Val]) -> Result<Val, String> {
    let name = match args.first() {
        Some(Val::String(s)) => s.as_str(),
        _ => "world",
    };
    Ok(Val::String(format!("hello, {name}")))
}

fn unless(args: &[Form]) -> Result<Form, String> {
    if args.len() < 2 {
        return Err("unless needs a test and a body".into());
    }
    let form = Form::Call(vec![
        Form::Symbol("if".into()),
        Form::Call(vec![Form::Symbol("not".into()), args[0].clone()]),
        args[1].clone(),
    ]);
    Ok(Form::Call(vec![form]))
}

fn retain(b: Vec<u8>) -> i32 {
    if b.is_empty() {
        let mut g = RET.lock().unwrap();
        *g = b;
        return 0;
    }
    let mut g = RET.lock().unwrap();
    *g = b;
    g.as_ptr() as i32
}

fn read_guest(ptr: i32, len: i32) -> Vec<u8> {
    if len <= 0 || ptr == 0 {
        return Vec::new();
    }
    unsafe { std::slice::from_raw_parts(ptr as *const u8, len as usize).to_vec() }
}

#[no_mangle]
pub extern "C" fn writ_abi() -> i32 {
    1
}

#[no_mangle]
pub extern "C" fn writ_alloc(n: i32) -> i32 {
    let n = if n < 0 { 0 } else { n as usize };
    let mut v = vec![0u8; n];
    let ptr = if n == 0 {
        0
    } else {
        v.as_mut_ptr() as i32
    };
    ALLOCS.lock().unwrap().push(v);
    ptr
}

#[no_mangle]
pub extern "C" fn writ_package() -> i32 {
    retain(package_table())
}

#[no_mangle]
pub extern "C" fn writ_call(kind: i32, name_ptr: i32, name_len: i32, args_ptr: i32, args_len: i32) -> i32 {
    // Copy name/args out before dropping host writ_alloc buffers.
    let name = String::from_utf8_lossy(&read_guest(name_ptr, name_len)).into_owned();
    let args_blob = read_guest(args_ptr, args_len);
    ALLOCS.lock().unwrap().clear();
    let out = (|| -> Result<Vec<u8>, String> {
        if kind == CALL_MACRO {
            let mut r = R::new(&args_blob);
            let form = dec_form(&mut r)?;
            let Form::Call(items) = form else {
                return Err("args must be a list".into());
            };
            if name != "unless" {
                return Err(format!("unknown macro {name}"));
            }
            let result = unless(&items)?;
            let mut w = W::new();
            w.enc_form(&result)?;
            return Ok(w.b);
        }
        if kind != CALL_FUNC {
            return Err("unknown call kind".into());
        }
        let mut r = R::new(&args_blob);
        let args = dec_val(&mut r)?;
        let Val::Call(items) = args else {
            return Err("args must be a list".into());
        };
        if name != "greet" {
            return Err(format!("unknown func {name}"));
        }
        let result = greet(&items)?;
        let mut w = W::new();
        w.enc_val(&result)?;
        Ok(w.b)
    })();
    match out {
        Ok(b) => retain(b),
        Err(e) => retain(enc_err(&e)),
    }
}
