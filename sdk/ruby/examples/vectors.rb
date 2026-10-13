# frozen_string_literal: true
require_relative '../lib/licentivo'

fixture = JSON.parse(File.read(ARGV[0] || 'tests/lease-vectors.json'))
fixture.fetch('vectors').each do |vector|
  actual = Licentivo.verify_lease(vector['envelope'], fixture['trusted_keys'],
    vector['product_id'], vector['device_id'], vector['now'])
  abort "Ruby vector failed: #{vector['name']}" unless actual == vector['valid']
end
puts "Ruby security core: #{fixture['vectors'].length} lease vectors passed"
