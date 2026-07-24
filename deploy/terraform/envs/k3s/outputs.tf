output "public_ip" { value = module.ec2_k3s.public_ip }
output "instance_id" { value = module.ec2_k3s.instance_id }
output "ssh_command" { value = module.ec2_k3s.ssh_command }