# emberkit/atlas.hcl
# Variable block removed as hclsimple in emberkit cannot parse it.
# The URL is not used by emberkit's parsing logic, only the migration dir.

env "local" {
  # URL removed as it's not needed for emberkit's parsing and causes issues with hclsimple.

  # Define the migration directory.
  migration {
    dir = "file://migrations" # Path relative to the atlas.hcl file
  }

  # Format block removed as it's not needed for emberkit's parsing and causes issues with hclsimple.
}

# Optional: Define where the desired schema state is stored.
# schema "public" {
#   src = "file://schema.hcl"
# }