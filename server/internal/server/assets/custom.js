// Navigation bar handler — verbatim copy of the working pattern from
// reference ghokun/appletv3-iptv. ATV3's sample-xml framework calls this
// for every <viewWithNavigationBar onNavigate="handleNavbarNavigate(event)">
// click.
function handleNavbarNavigate(event) {
  var navId = event.navigationItemId;
  var docUrl = document.getElementById(navId).getElementByTagName('url').textContent;
  new ATVUtils.Ajax({
    'url': docUrl,
    'success': function (xhr) {
      event.success(xhr.responseXML);
    },
    'failure': function (status, xhr) {
      event.failure('Navigation failed to load.');
    }
  });
  event.onCancel = function () {};
}
