# A directory on a mounted volume (PVC, EBS or EFS) under the server's storage.allowedRoots.
resource "entrepot_blob_store" "efs" {
  name = "shared"
  type = "file"
  file = { path = "/data/shared" }
}

# An S3 bucket. Leave access_key_id unset to use IRSA / the AWS default credential chain.
resource "entrepot_blob_store" "s3" {
  name = "artifacts"
  type = "s3"
  s3 = {
    bucket             = "my-entrepot-artifacts"
    region             = "eu-west-2"
    prefix             = "prod"
    redirect_downloads = true
  }
}
