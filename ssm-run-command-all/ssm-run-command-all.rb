require 'pp'
require 'aws-sdk-ssm'
require 'json'

document_name, parameters = ARGV

ssm = Aws::SSM::Client.new
resp = ssm.describe_instance_information(
  filters: [
    {key: "PlatformTypes", values: ['Linux']},
	],
)

instance_ids = resp.flat_map {|page| page.instance_information_list.map(&:instance_id) }
instance_ids.each_slice(50) do |ids|
  puts "Sending a command to #{ids}"
  resp = ssm.send_command(
		instance_ids: ids,
    document_name: document_name,
    parameters: JSON.parse(parameters),
    max_errors: '0',
    max_concurrency: '10',
	)
  command_id = resp.command.command_id

  loop do
    resp = ssm.list_commands({
      command_id: command_id,
    })
    status = resp.commands[0].status
    puts "#{command_id}: #{status}"
    unless %w!Pending InProgress!.include?(status)
      break
    end
    sleep 10
  end
end
