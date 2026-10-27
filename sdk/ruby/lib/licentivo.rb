# frozen_string_literal: true
require 'fiddle'
require 'json'
require 'monitor'

module Licentivo
  VERSION = '0.11.5'

  class Error < StandardError
    attr_reader :code, :http_status, :request_id, :retryable, :retry_after_seconds
    def initialize(message, code: 'SDK_ERROR', http_status: 0, request_id: '', retryable: false, retry_after_seconds: 0)
      @code, @http_status, @request_id = code, http_status, request_id
      @retryable, @retry_after_seconds = retryable, retry_after_seconds
      super("#{code}: #{message}#{request_id.empty? ? '' : " (request #{request_id})"}")
    end
  end

  Execution = Struct.new(:value, :executed, keyword_init: true) do
    def replay? = !executed
  end

  # This library does not register Ruby callbacks on the native heartbeat thread.
  # Local status reads can be marshalled by the application to its own UI thread.
  module Native
    module_function
    def library
      @library ||= begin
        suffix = case RUBY_PLATFORM
                 when /mswin|mingw/ then 'dll'
                 when /darwin/ then 'dylib'
                 else 'so'
                 end
        folder = File.expand_path('licentivo/native', __dir__)
        path = ENV['LICENTIVO_NATIVE_LIBRARY'] || [suffix, 'so', 'dll', 'dylib'].uniq.map { |ext| File.join(folder, "licentivo_native.#{ext}") }.find { |file| File.file?(file) }
        path ||= File.join(folder, "licentivo_native.#{suffix}")
        unless File.file?(path)
          raise LoadError, 'Licentivo native core is missing. Run ruby sdk/ruby/build_native.rb for a source bundle, or install the compiled gem. See sdk/ruby/README.md.'
        end
        if suffix == 'dll'
          # Ruby's Windows loader does not search process PATH for dependent
          # DLLs. Load the matching libcurl explicitly and keep its handle alive.
          candidates = Dir[File.join(File.dirname(path), 'libcurl*.dll')]
          if ENV['LICENTIVO_CURL_ROOT']
            candidates.concat(Dir[File.join(File.expand_path(ENV['LICENTIVO_CURL_ROOT']), 'bin', 'libcurl*.dll')])
          end
          @curl_library = Fiddle.dlopen(candidates.first) unless candidates.empty?
        end
        Fiddle.dlopen(path)
      end
    end
    def bind(name, arguments, result)
      # Release the GVL while the core uses the network. Fiddle closures acquire
      # it again before entering a Ruby block; native heartbeats never enter Ruby.
      Fiddle::Function.new(library[name], arguments, result, need_gvl: false)
    end
    def functions
      @functions ||= begin
        p, i, q = Fiddle::TYPE_VOIDP, Fiddle::TYPE_INT, Fiddle::TYPE_LONG_LONG
        signatures = {
          init: ['ln_init_file_with_license', [p,p], p],
          init_json: ['ln_init', [p], p],
          init_product: ['ln_init_product', [p,p,p], p],
          start: ['ln_start', [p], i], destroy: ['ln_destroy', [p], Fiddle::TYPE_VOID],
          last_error: ['ln_last_error', [p], p], error_info: ['ln_get_error_info', [p,p], i],
          status: ['ln_status', [p], p], feature_status: ['ln_feature_status', [p,p], p],
          run_feature: ['ln_run_feature', [p,p,p,p], i],
          run_metered_feature: ['ln_run_metered_feature', [p,p,q,p,p,p], i],
          resolve_metered_feature: ['ln_resolve_metered_feature', [p,p,i], i],
          validate: ['ln_validate', [p], i], heartbeat: ['ln_heartbeat', [p], i],
          refresh_online: ['ln_refresh_online', [p], i], deactivate: ['ln_deactivate', [p], i],
          logout: ['ln_logout', [p], i], retry_pending_logouts: ['ln_retry_pending_logouts', [p], i], logout_pending: ['ln_logout_pending', [p], i],
          reserve: ['ln_reserve', [p,p,q,p], p], consume: ['ln_consume', [p,p,q,p], p],
          commit: ['ln_commit', [p,p,p], p], cancel: ['ln_cancel', [p,p,p], p],
          offline_request: ['ln_offline_request', [p,p], p], import_offline: ['ln_import_offline', [p,p,p], i],
          free_string: ['ln_free_string', [p], Fiddle::TYPE_VOID],
          json: ['ln_json', [p], p], delete_json: ['cJSON_Delete', [p], Fiddle::TYPE_VOID],
          verify: ['ln_verify', [p,p,p,p,q], i]
        }
        signatures.transform_values { |name, args, result| bind(name, args, result) }
      end
    end
    def call(name, *arguments) = functions.fetch(name).call(*arguments)
    def input(value)
      value = String(value)
      raise ArgumentError, 'Input must not contain NUL bytes' if value.include?("\0")
      value + "\0"
    end
    def text(pointer) = pointer.null? ? '' : pointer.to_s.force_encoding(Encoding::UTF_8)
    def owned_json(pointer)
      raise Error.new('Native core returned no result', code: 'INVALID_RESPONSE') if pointer.null?
      begin
        JSON.parse(pointer.to_s)
      ensure
        call(:free_string, pointer)
      end
    end
  end

  class Client
    def self.open_config(config, &block)
      raise ArgumentError, 'Provide an in-code configuration Hash' unless config.is_a?(Hash)
      open_with_license(config, &block)
    end
    def self.open_product(product_id, trusted_keys, customer_key = '')
      client = new(product_id, customer_key, trusted_keys: trusted_keys)
      begin
        client.start
      rescue Exception
        client.close
        raise
      end
      return client unless block_given?
      begin
        yield client
      ensure
        client.close
      end
    end

    def self.open_with_license(path, customer_key = '')
      client = new(path, customer_key)
      begin
        client.start
      rescue Exception
        client.close
        raise
      end
      return client unless block_given?
      begin
        yield client
      ensure
        client.close
      end
    end

    def initialize(path, customer_key = '', trusted_keys: nil)
      @lock, @in_callback = Monitor.new, false
      @native = if path.is_a?(Hash)
                  Native.call(:init_json, Native.input(JSON.generate(path)))
                elsif trusted_keys.nil?
                  Native.call(:init, Native.input(path), Native.input(customer_key))
                else
                  Native.call(:init_product, Native.input(path), Native.input(JSON.generate(trusted_keys)), Native.input(customer_key.strip))
                end
      if @native.null?
        @native = nil
        raise Error.new('Check product ID, public keys and local storage', code: 'CONFIGURATION_ERROR')
      end
    end

    def start = checked(:start)
    def validate = checked(:validate)
    def heartbeat = checked(:heartbeat)
    def refresh_online = checked(:refresh_online)
    def deactivate = logout
    def logout
      @signed_out = true
      checked(:logout)
    end
    def retry_pending_logouts = checked(:retry_pending_logouts)
    def logout_pending? = @lock.synchronize { Native.call(:logout_pending, active) != 0 }
    def status = @signed_out ? signed_out_status : read_json(:status)
    def feature_status(feature) = @signed_out ? signed_out_status : read_json(:feature_status, Native.input(feature))
    def reserve(feature, quantity, job_id) = read_json(:reserve, Native.input(feature), integer(quantity), Native.input(job_id))
    def consume(feature, quantity, job_id) = read_json(:consume, Native.input(feature), integer(quantity), Native.input(job_id))
    def commit(reservation_id, job_id) = read_json(:commit, Native.input(reservation_id), Native.input(job_id))
    def cancel(reservation_id, job_id) = read_json(:cancel, Native.input(reservation_id), Native.input(job_id))
    def offline_request(operation = 'activate') = read_json(:offline_request, Native.input(operation))
    def import_offline(request, response) = checked(:import_offline, Native.input(JSON.generate(request)), Native.input(JSON.generate(response)))
    def resolve_metered_feature(job_id, business_succeeded)
      unless business_succeeded == true || business_succeeded == false
        raise ArgumentError, 'Determine the real business outcome; pass true or false'
      end
      checked(:resolve_metered_feature, Native.input(job_id), business_succeeded ? 1 : 0)
    end

    def run_feature(feature, &action)
      execution = run_callback(:run_feature, Native.input(feature), &action)
      execution.value
    end
    def run_metered_feature(feature, quantity, job_id, &action)
      run_callback(:run_metered_feature, Native.input(feature), integer(quantity), Native.input(job_id), &action)
    end

    def close
      @lock.synchronize do
        raise Error.new('Do not close this client inside its business callback', code: 'WORKFLOW_BUSY') if @in_callback
        if @native
          Native.call(:destroy, @native)
          @native = nil
        end
      end
      nil
    end

    private
    def signed_out_status = { 'state' => 'released', 'allowed' => false, 'code' => 'LAUNCH_LOGIN_REQUIRED', 'request_id' => '', 'retryable' => false, 'lease_valid_until' => 0 }
    def integer(value)
      raise ArgumentError, 'Quantity must be a positive signed 64-bit integer' unless value.is_a?(Integer) && value.positive? && value <= 0x7fff_ffff_ffff_ffff
      value
    end
    def active
      raise Error.new('Client is closed', code: 'CLIENT_CLOSED') unless @native
      @native
    end
    def error
      # ln_error_info: int, char[80], char[128], int, int; the shared ABI uses
      # 32-bit ints on supported Windows/Linux/macOS targets.
      info = Fiddle::Pointer.malloc(220, Fiddle::RUBY_FREE)
      info[0,220] = "\0" * 220
      Native.call(:error_info, @native, info)
      Error.new('The licensing operation did not complete',
        code: info[4,80].split("\0", 2).first,
        http_status: info[0,4].unpack1('i'), request_id: info[84,128].split("\0", 2).first,
        retryable: info[212,4].unpack1('i') != 0, retry_after_seconds: info[216,4].unpack1('i'))
    end
    def checked(name, *arguments)
      @lock.synchronize do
        raise Error.new('Complete a fresh login', code: 'LAUNCH_LOGIN_REQUIRED') if @signed_out && ![:logout, :deactivate, :retry_pending_logouts].include?(name)
        raise error if Native.call(name, active, *arguments) == 0
      end
      true
    end
    def read_json(name, *arguments)
      @lock.synchronize do
        pointer = Native.call(name, active, *arguments)
        raise error if pointer.null?
        Native.owned_json(pointer)
      end
    end
    def run_callback(name, *arguments)
      raise Error.new('Complete a fresh login', code: 'LAUNCH_LOGIN_REQUIRED') if @signed_out
      raise ArgumentError, 'Supply your business operation as a block' unless block_given?
      @lock.synchronize do
        raise Error.new('A business callback is already running', code: 'WORKFLOW_BUSY') if @in_callback
        execution = Execution.new(executed: false)
        business_error = nil
        callback = Fiddle::Closure::BlockCaller.new(Fiddle::TYPE_INT, [Fiddle::TYPE_VOIDP]) do |_context|
          @in_callback = true
          begin
            execution.executed = true
            execution.value = yield
            1
          rescue Exception => exception
            # Exceptions never cross the C boundary. The native workflow settles
            # cancellation (or journals a pending cancellation) before re-raising.
            business_error = exception
            0
          ensure
            @in_callback = false
          end
        end
        ok = Native.call(name, active, *arguments, callback, 0)
        raise business_error if business_error
        raise error if ok == 0
        execution
      end
    end
  end

  def self.verify_lease(envelope, pins, product, device, now)
    parsed_envelope = Native.call(:json, Native.input(JSON.generate(envelope)))
    parsed_pins = Native.call(:json, Native.input(JSON.generate(pins)))
    begin
      Native.call(:verify, parsed_envelope, parsed_pins, Native.input(product), Native.input(device), Integer(now)) != 0
    ensure
      Native.call(:delete_json, parsed_envelope)
      Native.call(:delete_json, parsed_pins)
    end
  end
end
