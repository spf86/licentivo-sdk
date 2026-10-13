# frozen_string_literal: true
# Compiles the shared licensing core without downloading anything or modifying
# system settings. Source bundles ship canonical copies under native/.
require 'fileutils'
require 'open3'
require 'rbconfig'
require 'shellwords'

package = File.expand_path(__dir__)
bundled = File.join(package, 'native')
core = File.file?(File.join(bundled, 'licentivo.c')) ? bundled : File.expand_path('../c', package)
windows = RbConfig::CONFIG['host_os'].match?(/mswin|mingw/)
mac = RbConfig::CONFIG['host_os'].include?('darwin')
extension = windows ? 'dll' : mac ? 'dylib' : 'so'
output = ARGV[0] || File.join(package, 'lib/licentivo/native', "licentivo_native.#{extension}")
FileUtils.mkdir_p(File.dirname(output))
compiler = Shellwords.split(ENV.fetch('CC', windows ? 'gcc' : 'cc'))
command = compiler + ['-std=c11', '-O2', "-I#{core}"]
command << '-fPIC' unless windows
command << (mac ? '-dynamiclib' : '-shared')
command << '-Wl,--export-all-symbols' if windows
curl = ENV['LICENTIVO_CURL_ROOT']
if curl && !curl.empty?
  command << "-I#{File.join(curl, 'include')}"
  command << "-L#{File.join(curl, 'lib')}"
elsif windows
  abort 'Set LICENTIVO_CURL_ROOT to your matching libcurl development folder. See README.md.'
elsif system('pkg-config', '--exists', 'libcurl', 'openssl', out: File::NULL, err: File::NULL)
  flags, status = Open3.capture2('pkg-config', '--cflags', '--libs', 'libcurl', 'openssl')
  abort 'pkg-config failed' unless status.success?
  # Library options belong after the object/source files in the linker command.
  includes, libraries = Shellwords.split(flags).partition { |flag| flag.start_with?('-I', '-D') }
  command.concat(includes)
end
command.concat(%w[licentivo.c vendor/cJSON.c vendor/monocypher.c vendor/monocypher-ed25519.c].map { |file| File.join(core, file) })
command.concat(libraries || ['-lcurl'])
command.concat(windows ? %w[-lbcrypt -ladvapi32] : %w[-lcrypto -lpthread -lm])
command.concat(['-o', output])
abort 'Native core build failed. Install a C11 compiler and libcurl/OpenSSL development files.' unless system(*command)
if windows && curl
  dlls = Dir[File.join(File.expand_path(curl), 'bin', 'libcurl*.dll')]
  abort 'The matching runtime libcurl DLL is missing' if dlls.empty?
  dlls.each { |dll| FileUtils.cp(dll, File.dirname(output)) }
  license = [File.join(curl, 'COPYING'), File.join(curl, 'COPYING.txt')].find { |file| File.file?(file) }
  abort 'The libcurl license is missing' unless license
  FileUtils.cp(license, File.join(File.dirname(output), 'libcurl-LICENSE'))
end
puts "Native licensing core built: #{output}"
