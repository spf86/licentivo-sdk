use std::{env, path::PathBuf};

fn main() {
    let package = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
    let bundled = package.join("native");
    let core = if bundled.join("licentivo.c").is_file() { bundled } else { package.join("../c") };
    let target_os = env::var("CARGO_CFG_TARGET_OS").unwrap();
    let target_env = env::var("CARGO_CFG_TARGET_ENV").unwrap_or_default();
    let mut build = cc::Build::new();
    build.include(&core).std("c11").warnings(false);
    for name in ["licentivo.c", "vendor/cJSON.c", "vendor/monocypher.c", "vendor/monocypher-ed25519.c"] {
        build.file(core.join(name));
    }
    println!("cargo:rerun-if-changed={}", core.display());
    println!("cargo:rerun-if-env-changed=LICENTIVO_CURL_ROOT");
    if let Some(root) = env::var_os("LICENTIVO_CURL_ROOT") {
        let root = PathBuf::from(root);
        build.include(root.join("include"));
        println!("cargo:rustc-link-search=native={}", root.join("lib").display());
    } else if target_os == "windows" {
        panic!("Set LICENTIVO_CURL_ROOT to a libcurl development package matching your Rust target. See README.md.");
    } else {
        // pkg-config is optional on standard Linux installations. Native headers
        // and libraries still have to be installed; no files are downloaded here.
        for package in ["libcurl", "openssl"] {
            if let Ok(library) = pkg_config::Config::new().cargo_metadata(false).probe(package) {
                for path in library.include_paths { build.include(path); }
                for path in library.link_paths { println!("cargo:rustc-link-search=native={}", path.display()); }
            }
        }
    }
    if target_env == "msvc" {
        panic!("The current C11 core supports Windows GNU/MinGW builds. Use the x86_64-pc-windows-gnu Rust target; MSVC acceptance is pending.");
    }
    build.compile("licentivo_core");
    println!("cargo:rustc-link-lib=curl");
    if target_os == "windows" {
        println!("cargo:rustc-link-lib=bcrypt");
        println!("cargo:rustc-link-lib=advapi32");
    } else {
        println!("cargo:rustc-link-lib=crypto");
        println!("cargo:rustc-link-lib=pthread");
        println!("cargo:rustc-link-lib=m");
    }
}
