import re

with open('../internal/service/third_party_service.go', 'r') as f:
    content = f.read()

# Define the boundaries (this is a simplified approach, in reality AST parsing is safer, but regex works for well-formatted go code)
# We will just manually copy-paste for safety since python script might break the code.
