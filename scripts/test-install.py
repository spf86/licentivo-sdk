#!/usr/bin/env python3
"""Install release packages in independent projects; never contact a license service."""
import argparse
import hashlib
import json
import os
import pathlib
import platform
import shutil
import subprocess
import tempfile
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
VERSION = (ROOT / "VERSION").read_text().strip()


def run(*command, cwd, expect_failure=False):
    print("+ " + " ".join(map(str, command)), flush=True)
    environment = os.environ.copy()
    if os.name == "nt" and pathlib.Path(str(command[0])).name.lower() in ("powershell.exe", "pwsh.exe"):
        # A PowerShell 7 parent can pass incompatible module paths to 5.1.
        # Let the selected shell construct its own standard module search path.
        environment = {key: value for key, value in environment.items() if key.casefold() != "psmodulepath"}
    result = subprocess.run(list(map(str, command)), cwd=cwd, env=environment, capture_output=expect_failure, text=expect_failure)
    if expect_failure:
        if result.returncode == 0:
            raise AssertionError("Unsafe installation unexpectedly succeeded")
        return result.stdout + result.stderr
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)


def install(language, folder, packages, *extra, expect_failure=False):
    if os.name == "nt":
        command = ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ROOT / "install.ps1", "-Language", language, "-PackageDirectory", packages]
    else:
        command = ["bash", ROOT / "install.sh", language, "--package-dir", packages]
    return run(*command, *extra, cwd=folder, expect_failure=expect_failure)


def option(windows, posix):
    return windows if os.name == "nt" else posix


def native(args, work, packages):
    target = 'windows-x64-' + ('mingw' if args.curl_root else 'msvc') if os.name == 'nt' else platform.system().lower() + '-x64'
    names = list(packages.glob(f"licentivo-native-{VERSION}-{target}.zip"))
    if len(names) != 1:
        raise ValueError("Choose a package directory containing one matching native platform package")
    # The regular consumer check uses the prefix extracted by the actual common
    # installer, rather than bypassing it with a manual source installation.
    prefix = work / f"c/.licentivo-sdk/c/v{VERSION}/native"
    if not prefix.exists():
        prefix = work / "native"
        with zipfile.ZipFile(names[0]) as archive:
            archive.extractall(prefix)
    folder = work / "native-consumer"
    folder.mkdir()
    (folder / "consumer.c").write_text('#include <licentivo.h>\n#include <stdio.h>\nint main(void){ cJSON *j=ln_json("{}"); if(!j)return 1; cJSON_Delete(j); puts("C package consumer passed"); return 0; }\n')
    (folder / "consumer.cpp").write_text('#include <licentivo.hpp>\n#include <iostream>\nint main(){ cJSON *j=ln_json("{}"); if(!j)return 1; cJSON_Delete(j); std::cout<<"C++ package consumer passed\\n"; }\n')
    if shutil.which("cmake"):
        (folder / "CMakeLists.txt").write_text('''cmake_minimum_required(VERSION 3.18)
project(independent_consumer LANGUAGES C CXX)
find_package(Licentivo 0.10 REQUIRED)
add_executable(consumer_c consumer.c)
target_link_libraries(consumer_c PRIVATE Licentivo::C)
add_executable(consumer_cpp consumer.cpp)
target_link_libraries(consumer_cpp PRIVATE Licentivo::CPP)
set_target_properties(consumer_cpp PROPERTIES CXX_STANDARD 17)
''')
        command = ["cmake", "-S", folder, "-B", folder / "build", f"-DCMAKE_PREFIX_PATH={prefix}"]
        if os.name == "nt" and args.curl_root:
            curl = pathlib.Path(args.curl_root)
            command += ["-G", "MinGW Makefiles", f"-DCURL_INCLUDE_DIR={curl / 'include'}", f"-DCURL_LIBRARY={curl / 'lib/libcurl.dll.a'}"]
        run(*command, cwd=work)
        run("cmake", "--build", folder / "build", "--config", "Release", cwd=work)
        for name in ("consumer_c", "consumer_cpp"):
            executable = folder / "build" / (name + (".exe" if os.name == "nt" else ""))
            if not executable.exists() and os.name == "nt":
                executable = folder / "build/Release" / (name + ".exe")
            run(executable, cwd=work)
    elif os.name == "nt" and args.curl_root:
        curl = pathlib.Path(args.curl_root)
        for compiler, file in (("gcc", "consumer.c"), ("g++", "consumer.cpp")):
            executable = folder / (file + ".exe")
            run(compiler, "-std=c11" if compiler == "gcc" else "-std=c++17", f"-I{prefix / 'include/licentivo/c'}", f"-I{prefix / 'include/licentivo/cpp'}", folder / file, prefix / "lib/liblicentivo_c.a", curl / "lib/libcurl.dll.a", "-lbcrypt", "-ladvapi32", "-o", executable, cwd=work)
            run(executable, cwd=work)
    else:
        raise ValueError("CMake is required for native consumer checks")


def rejection_checks(work, packages):
    bad = work / "tampered"
    bad.mkdir()
    name = f"licentivo-go-{VERSION}.zip"
    shutil.copyfile(packages / "SHA256SUMS", bad / "SHA256SUMS")
    (bad / name).write_bytes((packages / name).read_bytes() + b"tampered")
    consumer = work / "tampered-consumer"
    consumer.mkdir()
    output = install("go", consumer, bad, option("-DownloadOnly", "--download-only"), expect_failure=True)
    if "SHA-256 mismatch" not in output or (consumer / f".licentivo-sdk/go/v{VERSION}/source").exists():
        raise AssertionError("Corrupted package was not rejected before extraction")
    with zipfile.ZipFile(bad / name, "w") as z:
        z.writestr("../../escape.txt", "must never be extracted")
    digest = hashlib.sha256((bad / name).read_bytes()).hexdigest()
    (bad / "SHA256SUMS").write_text(digest + "  " + name + "\n")
    output = install("go", consumer, bad, option("-DownloadOnly", "--download-only"), expect_failure=True)
    if "Unsafe path" not in output or list(consumer.rglob("escape.txt")):
        raise AssertionError("ZIP traversal was not rejected")
    print("Installer checksum and ZIP traversal rejection passed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--packages", required=True, type=pathlib.Path)
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--dotnet", default="dotnet")
    parser.add_argument("--curl-root", default=os.environ.get("LICENTIVO_CURL_ROOT", ""))
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--native-only", action="store_true")
    modes.add_argument("--bindings-only", action="store_true")
    args = parser.parse_args()
    packages = args.packages.resolve()
    if args.curl_root:
        args.curl_root = str(pathlib.Path(args.curl_root).resolve())
        os.environ["PATH"] = str(pathlib.Path(args.curl_root) / "bin") + os.pathsep + os.environ["PATH"]
    if args.java_home:
        args.java_home = str(pathlib.Path(args.java_home).resolve())
    dotnet = str(pathlib.Path(shutil.which(args.dotnet) or args.dotnet).resolve())
    os.environ["PATH"] = str(pathlib.Path(dotnet).parent) + os.pathsep + os.environ["PATH"]
    manifest = json.loads((packages / "manifest.json").read_text())
    if manifest["version"] != VERSION:
        raise ValueError("Package manifest version mismatch")
    for item in manifest["artifacts"]:
        if pathlib.Path(item["name"]).name != item["name"] or hashlib.sha256((packages / item["name"]).read_bytes()).hexdigest() != item["sha256"]:
            raise ValueError("Release asset checksum mismatch")
    (ROOT / ".local").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="consumer-check-", dir=ROOT / ".local") as temp:
        work = pathlib.Path(temp)
        if not args.native_only:
            languages = ("rust", "ruby", "c", "cpp") if args.bindings_only else ("go", "java", "c", "cpp", "csharp", "python", "javascript", "rust", "ruby")
            for language in languages:
                folder = work / language
                folder.mkdir()
                install(language, folder, packages, option("-DownloadOnly", "--download-only"))
            if not args.bindings_only:
                folder = work / "go"
                (folder / "go.mod").write_text("module example.com/package-consumer\n\ngo 1.26.0\n")
                install("go", folder, packages)
                (folder / "main.go").write_text('package main\nimport("fmt"; sdk "github.com/spf86/licentivo-sdk")\nfunc main(){ var c *sdk.Client; fmt.Printf("Go package consumer: %T\\n",c) }\n')
                run("go", "run", ".", cwd=folder)
                folder = work / "python"
                run(os.sys.executable, "-m", "venv", folder / ".venv", cwd=folder)
                python = folder / (".venv/Scripts/python.exe" if os.name == "nt" else ".venv/bin/python")
                install("python", folder, packages, option("-PythonExe", "--python"), python)
                run(python, "-c", 'from licentivo import Client; import cryptography; print("Python package consumer passed", Client.__name__)', cwd=folder)
                folder = work / "javascript"
                (folder / "package.json").write_text('{"name":"independent-sdk-consumer","private":true,"type":"module"}\n')
                install("javascript", folder, packages)
                run("node", "--input-type=module", "-e", "import {Client} from '@licentivo/sdk'; if(typeof Client !== 'function')process.exit(1); console.log('Node package consumer passed')", cwd=folder)
                folder = work / "java"
                install("java", folder, packages)
                java_bin = pathlib.Path(args.java_home) / "bin" if args.java_home else None
                javac = java_bin / ("javac.exe" if os.name == "nt" else "javac") if java_bin else "javac"
                java = java_bin / ("java.exe" if os.name == "nt" else "java") if java_bin else "java"
                jar = folder / f".licentivo-sdk/java/v{VERSION}/licentivo-sdk-{VERSION}.jar"
                (folder / "Consumer.java").write_text('import licentivo.Client; public class Consumer { public static void main(String[] args) { System.out.println("Java package consumer passed " + Client.class.getName()); } }\n')
                run(javac, "-cp", jar, "Consumer.java", cwd=folder)
                run(java, "-cp", str(jar) + os.pathsep + str(folder), "Consumer", cwd=folder)
                folder = work / "csharp"
                (folder / "Consumer.csproj").write_text('<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n')
                (folder / "Program.cs").write_text('using Licentivo; System.Console.WriteLine("C# package consumer passed " + typeof(LicentivoClient).FullName);\n')
                # The installer must retain existing feeds and other NuGet settings.
                (folder / "NuGet.Config").write_text('<configuration><packageSources><add key="retained-source" value="https://api.nuget.org/v3/index.json" /></packageSources><config><add key="signatureValidationMode" value="accept" /></config></configuration>\n')
                install("csharp", folder, packages, option("-Project", "--project"), folder / "Consumer.csproj")
                if "retained-source" not in (folder / "NuGet.Config").read_text() or "signatureValidationMode" not in (folder / "NuGet.Config").read_text():
                    raise AssertionError("Existing NuGet configuration was lost")
                run(dotnet, "run", "--project", "Consumer.csproj", "--no-restore", cwd=folder)
            folder = work / "rust"
            (folder / "Cargo.toml").write_text('[package]\nname = "sdk-package-consumer"\nversion = "0.1.0"\nedition = "2021"\n[dependencies]\nserde_json = "1"\n')
            (folder / "src").mkdir()
            (folder / "src/main.rs").write_text('use licentivo::{Client,Error,Execution}; fn main(){ let _open = |p:&str,k:&str| Client::open_with_license(p,k); let _ = std::mem::size_of::<Error>(); let _ = Execution::<()>::AlreadyCompleted; println!("Rust package consumer passed"); }\n')
            install("rust", folder, packages, option("-Project", "--project"), folder / "Cargo.toml")
            if 'serde_json' not in (folder / "Cargo.toml").read_text():
                raise AssertionError("Existing Cargo dependency was lost")
            run("cargo", "run", cwd=folder)
            folder = work / "ruby"
            install("ruby", folder, packages)
            gem_directory = folder / f".licentivo-sdk/ruby/v{VERSION}/gems"
            environment_path = os.environ.get("GEM_PATH")
            os.environ["GEM_PATH"] = str(gem_directory)
            try:
                run("ruby", "-e", 'require "licentivo"; raise unless Licentivo::Native.functions[:run_metered_feature]; puts "Ruby package consumer passed"', cwd=folder)
            finally:
                if environment_path is None: os.environ.pop("GEM_PATH",None)
                else: os.environ["GEM_PATH"] = environment_path
            if not args.bindings_only:
                rejection_checks(work, packages)
        native(args, work, packages)
    print("Independent consumer installation checks passed", flush=True)


if __name__ == "__main__":
    main()
