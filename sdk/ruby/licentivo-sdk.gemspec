# frozen_string_literal: true
Gem::Specification.new do |spec|
  spec.name = 'licentivo-sdk'
  spec.version = '0.11.7'
  spec.authors = ['Licentivo']
  spec.summary = 'Application licensing, verified grants and durable metered workflows'
  spec.description = 'Licentivo adds license activation, machine-scoped device credentials, pinned signature verification, feature and expiry checks, policy heartbeats, permitted offline grants, floating seats and metered reserve/commit/cancel workflows to Ruby applications. Applications provide their activation UI, business block and durable results; the SDK manages the licensing protocol. See the documentation for pinned installation commands and working examples.'
  spec.homepage = 'https://www.licentivo.com/'
  spec.license = 'MIT'
  spec.required_ruby_version = '>= 3.1'
  spec.files = Dir['lib/**/*.rb', 'ext/**/*.rb', 'native/**/*', 'examples/*.rb'] + %w[README.md LICENSE NOTICE build_native.rb]
  spec.require_paths = ['lib']
  if ENV['LICENTIVO_GEM_PRECOMPILED'] == '1'
    spec.platform = Gem::Platform.local
    spec.files += Dir['lib/licentivo/native/*']
  else
    spec.extensions = ['ext/licentivo_native/extconf.rb']
  end
  spec.metadata['homepage_uri'] = 'https://www.licentivo.com/'
  spec.metadata['source_code_uri'] = 'https://github.com/spf86/licentivo-sdk'
  spec.metadata['documentation_uri'] = 'https://www.licentivo.com/en/docs.html#sdk'
  spec.metadata['allowed_push_host'] = 'https://rubygems.org'
end
