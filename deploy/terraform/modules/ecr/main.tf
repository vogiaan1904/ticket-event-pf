resource "aws_ecr_repository" "this" {
  for_each             = toset(var.image_names)
  name                 = each.value
  image_tag_mutability = "MUTABLE" # we push :latest + :<sha>
  image_scanning_configuration {
    scan_on_push = true
  }
  tags = var.tags
}

# Keep at most 10 tagged images, and expire untagged images after 3 days.
resource "aws_ecr_lifecycle_policy" "this" {
  for_each   = aws_ecr_repository.this
  repository = each.value.name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images older than 3 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 3
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the last 10 tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["latest", "sha-"]
          countType     = "imageCountMoreThan"
          countNumber   = 10
        }
        action = { type = "expire" }
      }
    ]
  })
}