output "instance_id" { value = aws_instance.k3s.id }
output "public_ip" { value = aws_instance.k3s.public_ip }
output "ssh_user" { value = "ec2-user" }
