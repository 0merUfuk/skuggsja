#!/usr/bin/env python3
"""Fault-inject the actual release workflow shell with synthetic files and fake gh."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import re
import shutil

REPO = Path(__file__).resolve().parents[1]
WORKFLOW_TEXT = (REPO / '.github/workflows/release.yml').read_text()
STAGE_NAMES = (
    'Create new release draft',
    'Upload and verify draft assets',
    'Publish complete draft and confirm immutability',
)


def extract_flow(workflow):
    """Read only the three expected literal run blocks; reject layout changes.

    This is deliberately not a YAML parser. actionlint validates the complete
    workflow separately; the strict indentation and names keep these tests tied
    to the actual publish commands without introducing a YAML dependency.
    """
    lines = workflow.splitlines()
    boundaries = [i for i, line in enumerate(lines) if line.startswith('      - ')]
    found = []
    for position, start in enumerate(boundaries):
        name = lines[start].removeprefix('      - name: ')
        if name not in STAGE_NAMES:
            continue
        end = boundaries[position + 1] if position + 1 < len(boundaries) else len(lines)
        block = lines[start + 1:end]
        markers = [i for i, line in enumerate(block) if line == '        run: |']
        if len(markers) != 1:
            raise ValueError(f'{name}: expected one literal run block')
        body = block[markers[0] + 1:]
        if not any(line.strip() for line in body):
            raise ValueError(f'{name}: empty run block')
        if any(line.strip() and not line.startswith('          ') for line in body):
            raise ValueError(f'{name}: unexpected indentation after run block')
        found.append((name, '\n'.join(line[10:] if line.strip() else '' for line in body) + '\n'))
    if tuple(name for name, _ in found) != STAGE_NAMES:
        raise ValueError('Expected each release stage exactly once in publication order')
    return [script for _, script in found]


FLOW = extract_flow(WORKFLOW_TEXT)

FAKE_GH = r'''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
mode=os.environ['FAKE_RELEASE_MODE']
root=Path(os.environ['FAKE_RELEASE_ROOT'])
with (root/'calls.jsonl').open('a') as f:f.write(json.dumps(args)+'\n')
paths=sorted((root/'dist').glob('*.tar.gz'))+sorted((root/'dist').glob('*.zip'))+[root/'dist/checksums.txt',root/'dist/skuggsja.rb']
if args[0]=='release':
 if args[2]!=os.environ['RELEASE_TAG'] or '--repo' not in args:sys.exit(94)
 if args[args.index('--repo')+1]!=os.environ['GITHUB_REPOSITORY']:sys.exit(95)
if args[:2]==['release','create']:
 if '--draft' not in args or '--verify-tag' not in args:sys.exit(90)
 sys.exit(1 if mode=='existing-release' else 0)
if args[:2]==['release','upload']:
 if '--clobber' in args:sys.exit(91)
 if set(args[5:])!={str(p.relative_to(root)) for p in paths}:sys.exit(96)
 sys.exit(1 if mode=='upload-failure' else 0)
if args[0]=='api':
 if args[1]!='repos/'+os.environ['GITHUB_REPOSITORY']+'/releases/tags/'+os.environ['RELEASE_TAG']:sys.exit(97)
 if '--jq' in args:
  if args[args.index('--jq')+1]!='.draft == false and .immutable == true':sys.exit(98)
  print('false' if mode in ['mutable-publication','publication-still-draft'] else 'true')
  sys.exit(0)
 data={'tag_name':os.environ['RELEASE_TAG'],'draft':True,'prerelease':False,'assets':[{'name':p.name,'size':p.stat().st_size,'state':'uploaded'} for p in paths]}
 if mode=='wrong-tag':data['tag_name']='v9.9.8'
 if mode=='already-published':data['draft']=False
 if mode=='prerelease':data['prerelease']=True
 if mode=='missing-asset':data['assets'].pop()
 if mode=='duplicate-asset':data['assets'][-1]=dict(data['assets'][0])
 if mode=='extra-asset':data['assets'].append({'name':'unexpected.rb','size':1,'state':'uploaded'})
 if mode=='wrong-size':data['assets'][0]['size']+=1
 if mode=='incomplete-upload':data['assets'][0]['state']='starter'
 print(json.dumps(data));sys.exit(0)
if args[:2]==['release','download']:
 if mode=='download-failure':sys.exit(1)
 folder=Path(args[args.index('--dir')+1]);folder.mkdir(exist_ok=True)
 for i,p in enumerate(paths):
  if mode=='missing-download' and i==0:continue
  data=p.read_bytes()
  if mode=='different-bytes' and i==0:data=b'!' + data[1:]
  (folder/p.name).write_bytes(data)
 sys.exit(0)
if args[:2]==['release','edit']:
 if '--draft=false' not in args or '--verify-tag' not in args:sys.exit(92)
 sys.exit(0)
sys.exit(93)
'''


class ReleaseFlow(unittest.TestCase):
    def run_flow(self, mode):
        with tempfile.TemporaryDirectory(prefix='skuggsja-release-flow-') as temporary:
            root = Path(temporary)
            (root / 'bin').mkdir()
            (root / 'dist').mkdir()
            (root / 'runner-temp').mkdir()
            executable = root / 'bin/gh'
            executable.write_text(FAKE_GH.replace('#!/usr/bin/env python3', '#!' + sys.executable, 1))
            (root / 'bin/python3').symlink_to(sys.executable)
            executable.chmod(0o700)
            for platform in ['darwin_amd64', 'darwin_arm64', 'linux_amd64', 'linux_arm64', 'windows_amd64', 'windows_arm64']:
                ext = 'zip' if platform.startswith('windows') else 'tar.gz'
                (root / 'dist' / f'skuggsja_9.9.9_{platform}.{ext}').write_bytes(('synthetic ' + platform).encode())
            for name in ['checksums.txt', 'skuggsja.rb', 'CHANGELOG.md']:
                (root / 'dist' / name).write_text('synthetic release test data\n')
            environment = dict(
                PATH=str(root / 'bin'),
                FAKE_RELEASE_MODE=mode, FAKE_RELEASE_ROOT=str(root),
                RELEASE_TAG='v9.9.9', GITHUB_REPOSITORY='0merUfuk/skuggsja',
                GH_TOKEN='synthetic-unusable-token', RUNNER_TEMP=str(root / 'runner-temp'))
            profile = root / 'network-denied.sb'
            profile.write_text('(version 1)\n(allow default)\n(deny network*)\n')
            completed = []
            for index, script in enumerate(FLOW):
                command = [shutil.which('bash'), '--noprofile', '--norc', '-e', '-o', 'pipefail', '-c', script]
                if sys.platform == 'darwin':
                    command = ['/usr/bin/sandbox-exec', '-f', str(profile), *command]
                result = subprocess.run(command, cwd=root, env=environment, text=True, capture_output=True, timeout=20)
                completed.append({'stage': index, 'exit_code': result.returncode,
                                  'stdout': result.stdout, 'stderr': result.stderr})
                if result.returncode:
                    break
            calls = [json.loads(line) for line in (root / 'calls.jsonl').read_text().splitlines()]
            self.last_evidence = {'mode': mode, 'stages': completed, 'gh_calls': calls,
                                  'network': 'OS denied' if sys.platform == 'darwin' else 'fake gh only'}
            return completed[-1]['exit_code'], calls

    def test_complete_draft_is_verified_before_publish(self):
        code, calls = self.run_flow('success')
        self.assertEqual(code, 0, self.last_evidence)
        self.assertEqual([call[:2] for call in calls], [
            ['release', 'create'], ['release', 'upload'],
            ['api', 'repos/0merUfuk/skuggsja/releases/tags/v9.9.9'],
            ['release', 'download'], ['release', 'edit'],
            ['api', 'repos/0merUfuk/skuggsja/releases/tags/v9.9.9'],
        ])
        self.assertIn('8/8 uploaded assets', self.last_evidence['stages'][1]['stdout'])

    def test_existing_release_is_never_uploaded_or_edited(self):
        code, calls = self.run_flow('existing-release')
        self.assertNotEqual(code, 0)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][:2], ['release', 'create'])

    def test_upload_failure_cannot_publish(self):
        self.reject_before_publish('upload-failure')

    def reject_before_publish(self, mode):
        code, calls = self.run_flow(mode)
        self.assertNotEqual(code, 0)
        self.assertFalse(any(call[:2] == ['release', 'edit'] for call in calls))

    def test_wrong_tag_cannot_publish(self): self.reject_before_publish('wrong-tag')
    def test_nondraft_cannot_publish(self): self.reject_before_publish('already-published')
    def test_prerelease_cannot_publish(self): self.reject_before_publish('prerelease')
    def test_missing_asset_cannot_publish(self): self.reject_before_publish('missing-asset')
    def test_duplicate_asset_cannot_publish(self): self.reject_before_publish('duplicate-asset')
    def test_extra_asset_cannot_publish(self): self.reject_before_publish('extra-asset')
    def test_wrong_size_cannot_publish(self): self.reject_before_publish('wrong-size')
    def test_incomplete_upload_cannot_publish(self): self.reject_before_publish('incomplete-upload')
    def test_download_failure_cannot_publish(self): self.reject_before_publish('download-failure')
    def test_same_size_wrong_bytes_cannot_publish(self): self.reject_before_publish('different-bytes')
    def test_missing_download_cannot_publish(self): self.reject_before_publish('missing-download')

    def test_mutable_publication_cannot_be_reported_successful(self):
        code, calls = self.run_flow('mutable-publication')
        self.assertNotEqual(code, 0)
        self.assertEqual(calls[-2][:2], ['release', 'edit'])

    def test_still_draft_cannot_be_reported_successful(self):
        code, calls = self.run_flow('publication-still-draft')
        self.assertNotEqual(code, 0)
        self.assertEqual(calls[-2][:2], ['release', 'edit'])

    def test_release_gate_and_existing_pins_are_retained(self):
        self.assertEqual(len(FLOW), 3)
        self.assertRegex(WORKFLOW_TEXT, r"(?m)^  publish:\n    name: Publish verified artifacts\n    if: github\.repository == '0merUfuk/skuggsja'\n    needs: verify$")
        self.assertLess(WORKFLOW_TEXT.index('      - name: Attest release artifacts'),
                        WORKFLOW_TEXT.index('      - name: Create new release draft'))
        self.assertFalse(any('--clobber' in script or 'release delete' in script for script in FLOW))
        actions = re.findall(r'(?m)^\s+(?:- )?uses: ([^ #]+)', WORKFLOW_TEXT)
        self.assertTrue(actions)
        for action in actions:
            if not action.startswith('./'):
                self.assertRegex(action, r'@[0-9a-f]{40}$')

    def test_extractor_rejects_missing_stage(self):
        with self.assertRaises(ValueError):
            extract_flow(WORKFLOW_TEXT.replace('Create new release draft', 'Unrecognized release stage'))

    def test_extractor_rejects_duplicate_stage(self):
        with self.assertRaises(ValueError):
            extract_flow(WORKFLOW_TEXT + '\n      - name: Create new release draft\n        run: |\n          true\n')

    def test_extractor_rejects_nonliteral_shell(self):
        with self.assertRaises(ValueError):
            extract_flow(WORKFLOW_TEXT.replace('        run: |', '        run: >'))


if __name__ == '__main__':
    unittest.main(verbosity=2)
