# frozen_string_literal: true
require 'mkmf'

package = File.expand_path('../..', __dir__)
core = File.join(package, 'native')
abort 'Bundled native core is missing' unless File.file?(File.join(core, 'licentivo.c'))
$INCFLAGS << " -I#{Shellwords.escape(core)}"
$VPATH.concat([core, File.join(core, 'vendor')])
$srcs = %w[licentivo.c cJSON.c monocypher.c monocypher-ed25519.c]
$CFLAGS << ' -std=c11'
curl = ENV['LICENTIVO_CURL_ROOT']
if curl && !curl.empty?
  dir_config('curl', File.join(curl, 'include'), File.join(curl, 'lib'))
end
abort 'libcurl development files are required' unless have_header('curl/curl.h') && have_library('curl', 'curl_easy_init')
if RbConfig::CONFIG['host_os'].match?(/mswin|mingw/)
  $DLDFLAGS << ' -Wl,--export-all-symbols'
  have_library('bcrypt')
  have_library('advapi32')
else
  abort 'OpenSSL development files are required' unless have_header('openssl/evp.h') && have_library('crypto', 'EVP_sha256')
  have_library('pthread')
  have_library('m')
end
# Fiddle loads this library directly. It contains no Ruby C API calls or Init_*
# hook; the target path is also used by the standalone source build script.
create_makefile('licentivo/native/licentivo_native')
