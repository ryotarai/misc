require 'pp'
require 'aws-sdk-ssm'
require 'json'
require 'optparse'

parser = OptionParser.new

document_name = nil
parameters = nil
instance_ids = nil

parser.on('-d VAL') {|v| document_name = v }
parser.on('-p VAL') {|v| parameters = v }
parser.on('-i VAL') {|v| instance_ids = v.split(',') }
parser.parse!(ARGV)

unless document_name && parameters
  puts "-d DOCUMENT_NAME -p PARAMETERS"
  abort
end

ssm = Aws::SSM::Client.new
resp = ssm.describe_instance_information(
  filters: [
    {key: "PlatformTypes", values: ['Linux']},
	],
)

unless instance_ids
  instance_ids = resp.flat_map {|page| page.instance_information_list.map(&:instance_id) }
end

instance_ids.shuffle.each_slice(50) do |ids|
  puts "Sending a command to #{ids}"
  resp = ssm.send_command(
		instance_ids: ids,
    document_name: document_name,
    parameters: JSON.parse(parameters),
    max_errors: '0',
    max_concurrency: '5',
	)
  command_id = resp.command.command_id

  loop do
    resp = ssm.list_commands({
      command_id: command_id,
    })
    status = resp.commands[0].status
    puts "#{command_id}: #{status}"
    unless %w!Pending InProgress!.include?(status)
      if status == 'Failed'
        # print failed command invocaiton
        ssm.list_command_invocations({
          command_id: command_id,
          filters: [{key: 'Status', value: 'Failed'}],
        }).command_invocations.each do |i|
          puts "#{i.instance_name} (#{i.instance_id}) #{i.status} / #{i.status_details}"
        end
      end
      break
    end
    sleep 30
  end
end
