"""Lists the framework calls in the shell's classes that are newer than
its minSdk, as Android Lint's NewApi check would: a call there throws
NoSuchMethodError on an older phone unless an SDK_INT check guards it.

Usage: python3 -I apicheck.py API_VERSIONS_XML CLASSES_JAR MIN_SDK
"""
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

api_xml, jar, min_sdk = sys.argv[1], sys.argv[2], float(sys.argv[3])

classes = {}
for c in ET.parse(api_xml).getroot().iter('class'):
    members = {}
    for m in c:
        if m.tag in ('method', 'field') and 'since' in m.attrib:
            members[m.get('name')] = float(m.get('since'))
    classes[c.get('name')] = (float(c.get('since', '1')), members)

def since(owner, name):
    """The API level a member arrived at, looking up through superclasses."""
    seen = set()
    todo = [owner]
    while todo:
        cls = todo.pop(0)
        if cls in seen or cls not in classes:
            continue
        seen.add(cls)
        cls_since, members = classes[cls]
        if name in members:
            return max(cls_since, members[name])
        for c in ET_EXTENDS.get(cls, []):
            todo.append(c)
    return classes.get(owner, (0, {}))[0]

ET_EXTENDS = {}
for c in ET.parse(api_xml).getroot().iter('class'):
    ET_EXTENDS[c.get('name')] = [e.get('name') for e in c if e.tag in ('extends', 'implements')]

names = subprocess.run(['unzip', '-Z1', jar], capture_output=True, text=True).stdout.split()
names = [n[:-6].replace('/', '.') for n in names if n.endswith('.class')]
out = subprocess.run(['javap', '-c', '-p', '-classpath', jar] + names, capture_output=True, text=True).stdout
ref = re.compile(r'// (Method|InterfaceMethod|Field) (android/[\w/$]+)\.([\w<>$]+):(\S+)')
current = ''
found = {}
for line in out.splitlines():
    if line.startswith(('class ', 'public class ', 'final class ', 'public final class ', 'abstract class ')) or ' class ' in line and line.endswith('{'):
        current = line.split(' class ')[1].split()[0]
    m = ref.search(line)
    if not m:
        continue
    kind, owner, name, desc = m.groups()
    key = name + desc if kind != 'Field' else name
    s = since(owner, key)
    if s > min_sdk:
        found.setdefault((owner, key, s), set()).add(current.split('$')[0])
for (owner, key, s), users in sorted(found.items(), key=lambda x: -x[0][2]):
    print(f'API {s:g}: {owner.replace("/", ".")}.{key}  (in {", ".join(sorted(users))})')
