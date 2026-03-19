import sys

def extract(filename, new_filename, methods, pkg="api"):
    with open(filename, 'r') as f:
        lines = f.readlines()

    imports = []
    in_import = False
    for line in lines:
        if line.startswith('import ('):
            in_import = True
            imports.append(line)
        elif in_import:
            imports.append(line)
            if line.strip() == ')':
                in_import = False
        elif line.startswith('import '):
            imports.append(line)

    extracted_lines = [f"package {pkg}\n\n"]
    extracted_lines.extend(imports)
    extracted_lines.append("\n")

    current_method = None
    method_lines = []
    in_method = False
    brace_count = 0

    new_original_lines = []

    for line in lines:
        if not in_method:
            is_start = False
            for method in methods:
                if line.startswith(f"func (") and f" {method}(" in line:
                    in_method = True
                    current_method = method
                    method_lines = [line]
                    brace_count = line.count('{') - line.count('}')
                    is_start = True
                    break
            
            if not is_start:
                is_struct = False
                for method in methods:
                    if line.startswith(f"type {method} struct") or line.startswith(f"type {method} interface"):
                        in_method = True
                        current_method = method
                        method_lines = [line]
                        brace_count = line.count('{') - line.count('}')
                        is_struct = True
                        break
                
                if not is_struct:
                    new_original_lines.append(line)
        else:
            method_lines.append(line)
            brace_count += line.count('{') - line.count('}')
            if brace_count == 0:
                in_method = False
                extracted_lines.extend(method_lines)
                extracted_lines.append("\n")
                current_method = None

    with open(new_filename, 'w') as f:
        f.writelines(extracted_lines)

    with open(filename, 'w') as f:
        f.writelines(new_original_lines)

if __name__ == "__main__":
    pkg_name = sys.argv[-1]
    methods_list = sys.argv[3:-1]
    extract(sys.argv[1], sys.argv[2], methods_list, pkg_name)
