output "instance_id" { value = aws_instance.k3s.id }
output "public_ip" { value = aws_instance.k3s.public_ip }
output "ssh_user" { value = "ec2-user" }
output "ssh_command" {
  value = "ssh -o StrictHostKeyChecking=accept-new -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@${aws_instance.k3s.public_ip}"
}